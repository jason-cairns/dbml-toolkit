package parser

import (
	"strings"
	"testing"

	"github.com/jason-cairns/dbml-toolkit/ast"
)

const corpus = `
Project my_app {
  database_type: 'PostgreSQL'
  Note: 'The whole thing'
}

// a comment
enum public.order_status {
  created
  pending [note: 'waiting']
  "in progress"
}

TablePartial base {
  id int [pk, increment]
  created_at timestamp [default: ` + "`now()`" + `]
}

Table users as U [headercolor: #3498DB] {
  ~base
  username "varchar(255)" [not null, unique, note: 'login']
  status order_status [not null]
  balance decimal(10,2) [default: -1]
  indexes {
    (username) [unique, name: 'idx_user']
    ` + "`lower(username)`" + `
  }
  checks {
    ` + "`balance >= 0`" + ` [name: 'nonneg']
  }
  Note: 'application users'
}

Table posts {
  id int [pk]
  user_id int [ref: > users.id, not null]
}

Ref author: posts.user_id > U.id [delete: cascade, update: no action]

Ref: users.id <> posts.id

TableGroup core [color: #eee, note: 'core tables'] {
  users
  posts
}

Note reminder [color: #F4D03F] {
  '''
  multi line
  note body
  '''
}

records users(id, username) {
  1, 'alice'
  2, 'bob'
}
`

func TestParseCorpus(t *testing.T) {
	f, diags := Parse("corpus.dbml", corpus)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	if len(f.Projects) != 1 || f.Projects[0].Note != "The whole thing" {
		t.Fatalf("project not parsed: %+v", f.Projects)
	}
	if len(f.Enums) != 1 || len(f.Enums[0].Values) != 3 {
		t.Fatalf("enum: %+v", f.Enums)
	}
	if f.Enums[0].Values[2].Name != "in progress" {
		t.Fatalf("quoted enum value: %q", f.Enums[0].Values[2].Name)
	}
	if len(f.Tables) != 2 {
		t.Fatalf("want 2 tables, got %d", len(f.Tables))
	}
	users := f.Tables[0]
	if users.Alias != "U" || users.Schema != "" {
		t.Fatalf("alias/schema: %+v", users)
	}
	if hc := settingVal2(users.Settings, "headercolor"); hc != "#3498DB" {
		t.Fatalf("headercolor: %q", hc)
	}
	if users.Note != "application users" {
		t.Fatalf("table note: %q", users.Note)
	}
	// ~base injects 2 columns; local adds 3 => 5 declared on users
	if len(users.Columns) != 3 {
		t.Fatalf("declared columns (excludes injected): %d", len(users.Columns))
	}
	if len(users.Injects) != 1 || users.Injects[0] != "base" {
		t.Fatalf("injects: %+v", users.Injects)
	}
	if len(users.Indexes) != 2 || len(users.Checks) != 1 {
		t.Fatalf("indexes/checks: %d %d", len(users.Indexes), len(users.Checks))
	}
	// balance default negative number
	bal := users.Columns[2]
	if bal.Name != "balance" || settingVal2(bal.Settings, "default") != "-1" {
		t.Fatalf("default negative: %+v", bal)
	}
	// refs: inline (posts.user_id) + author + anon = 3
	if len(f.Refs) != 3 {
		t.Fatalf("want 3 refs, got %d: %+v", len(f.Refs), f.Refs)
	}
	inline := f.Refs[0]
	if inline.Left.Table != "posts" || inline.Left.Columns[0] != "user_id" || inline.Op != ">" {
		t.Fatalf("inline ref: %+v", inline)
	}
	if f.Refs[1].Name != "author" || f.Refs[1].Settings == nil {
		t.Fatalf("named ref: %+v", f.Refs[1])
	}
	if f.Refs[2].Op != "<>" {
		t.Fatalf("many-to-many ref: %+v", f.Refs[2])
	}
	if len(f.Groups) != 1 || len(f.Groups[0].Members) != 2 || f.Groups[0].Note != "core tables" {
		t.Fatalf("group: %+v", f.Groups)
	}
	if len(f.Notes) != 1 || !strings.Contains(f.Notes[0].Text, "multi line") {
		t.Fatalf("sticky note: %+v", f.Notes)
	}
	if len(f.Records) != 1 || len(f.Records[0].Rows) != 2 || f.Records[0].Rows[0][1] != "alice" {
		t.Fatalf("records: %+v", f.Records)
	}
	if len(f.Partials) != 1 || len(f.Partials[0].Columns) != 2 {
		t.Fatalf("partial: %+v", f.Partials)
	}
}

func TestErrorRecovery(t *testing.T) {
	_, diags := Parse("bad.dbml", "Table t { id int } @@@ Table u { id int }")
	if len(diags) == 0 {
		t.Fatal("expected a diagnostic for the stray token")
	}
}

func settingVal2(ss []ast.Setting, name string) string {
	for _, s := range ss {
		if strings.EqualFold(s.Name, name) {
			return s.Value
		}
	}
	return ""
}
