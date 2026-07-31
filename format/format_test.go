package format

import "testing"

// fmtOK formats src and fails on any parse diagnostic.
func fmtOK(t *testing.T, src string) string {
	t.Helper()
	out, diags := Format("t.dbml", src)
	if len(diags) > 0 {
		t.Fatalf("unexpected diagnostics for %q: %v", src, diags)
	}
	return out
}

func TestFormatCanonical(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{
			name: "column name, type and settings columns all align",
			in:   "Table users{\nid int [pk,increment]\nemail varchar(255) [not null,unique]\ncreated_at timestamp\n}\n",
			want: "Table users {\n  id         int          [pk, increment]\n  email      varchar(255) [not null, unique]\n  created_at timestamp\n}\n",
		},
		{
			name: "unbracketed long type does not push the settings column out",
			in:   "Table t {\n  a int [pk]\n  b date [not null]\n  c decimal\n}\n",
			want: "Table t {\n  a int  [pk]\n  b date [not null]\n  c decimal\n}\n",
		},
		{
			name: "enum with value note",
			in:   "enum status{\nactive\ndone [note:'finished']\n}\n",
			want: "Enum status {\n  active\n  done [note: 'finished']\n}\n",
		},
		{
			name: "standalone ref canonical form",
			in:   "Ref{ posts.user_id > users.id }\n",
			want: "Ref: posts.user_id > users.id\n",
		},
		{
			name: "table group note normalised to a Note line at the top",
			in:   "TableGroup g [note:'core']{\nusers\nposts\n}\n",
			want: "TableGroup g {\n  Note: 'core'\n  users\n  posts\n}\n",
		},
		{
			name: "project note leads its body fields",
			in:   "Project p {\ndatabase_type:'PostgreSQL'\nNote:'hi'\n}\n",
			want: "Project p {\n  Note: 'hi'\n  database_type: 'PostgreSQL'\n}\n",
		},
		{
			name: "table note sits at the top of the block",
			in:   "Table t {\n  id int\n  Note: 'a table'\n}\n",
			want: "Table t {\n  Note: 'a table'\n  id int\n}\n",
		},
		{
			name: "collapse blank lines between blocks",
			in:   "Table a { id int }\n\n\n\nTable b { id int }\n",
			want: "Table a {\n  id int\n}\n\nTable b {\n  id int\n}\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := fmtOK(t, c.in)
			if got != c.want {
				t.Errorf("Format() mismatch\n--- got ---\n%s\n--- want ---\n%s", got, c.want)
			}
		})
	}
}

func TestFormatIdempotent(t *testing.T) {
	inputs := []string{
		"Table users{\nid int [pk,increment]\nemail varchar(255) [not null,unique,note:'x']\ncreated_at timestamp [default:`now()`]\n}\n",
		"Project p {\ndatabase_type:'PostgreSQL'\nNote:'hi'\n}\nenum e { a\nb }\n",
		"Note n {\n'''\n## Title\n- one\n- two\n'''\n}\n",
		"Table t {\n  a int\n  indexes {\n    (a, b) [name:'i']\n    `lower(a)`\n  }\n}\n",
	}
	for _, in := range inputs {
		once := fmtOK(t, in)
		twice := fmtOK(t, once)
		if once != twice {
			t.Errorf("not idempotent\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
		}
	}
}

func TestFormatPreservesComments(t *testing.T) {
	in := "// header\nTable users {\n  id int [pk] // the id\n  // about email\n  email varchar\n}\n"
	want := "// header\nTable users {\n  id    int [pk]  // the id\n  // about email\n  email varchar\n}\n"
	got := fmtOK(t, in)
	if got != want {
		t.Errorf("comment preservation mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	// And formatting the result again is stable.
	if again := fmtOK(t, got); again != got {
		t.Errorf("comment formatting not idempotent\n--- got ---\n%s\n--- again ---\n%s", got, again)
	}
}

func TestFormatRejectsSyntaxError(t *testing.T) {
	out, diags := Format("t.dbml", "Table users {\n  id int [\n")
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for a broken file")
	}
	if out != "" {
		t.Errorf("broken file should format to empty, got %q", out)
	}
}
