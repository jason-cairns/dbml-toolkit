package render

import "testing"

func TestFactory(t *testing.T) {
	for _, name := range []string{"", "d2", "graphviz", "dot"} {
		if _, ok := Get(name); !ok {
			t.Fatalf("engine %q not found", name)
		}
	}
	if e, _ := Get(""); e.Name() != "d2" {
		t.Fatalf("default engine = %s, want d2", e.Name())
	}
	if _, ok := Get("nope"); ok {
		t.Fatal("unknown engine should not resolve")
	}
}
