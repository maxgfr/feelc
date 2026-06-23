package loader

import "testing"

const cacheSrc = "model \"c\" {}\ninput x : number\ndecision d : number = x\n"

func TestCacheHitsAvoidRecompile(t *testing.T) {
	c := NewCache(8)
	cm1, h1, _, err := c.Compile([]byte(cacheSrc))
	if err != nil {
		t.Fatal(err)
	}
	// Same source again → cache hit, identical (pointer-equal) compiled model.
	cm2, h2, _, err := c.Compile([]byte(cacheSrc))
	if err != nil {
		t.Fatal(err)
	}
	if cm1 != cm2 {
		t.Error("cache hit should return the same compiled model pointer")
	}
	if h1 != h2 {
		t.Error("hashes differ across a cache hit")
	}
	if hits, calls := c.Stats(); hits != 1 || calls != 2 {
		t.Errorf("stats = (%d hits, %d calls), want (1, 2)", hits, calls)
	}
	// Result must match a direct compile (cache is transparent).
	_, h3, _, _ := Compile([]byte(cacheSrc))
	if h3 != h1 {
		t.Errorf("cached hash %s != direct compile hash %s", h1, h3)
	}
}

func TestCacheEvictionBounded(t *testing.T) {
	c := NewCache(2)
	for i, s := range []string{
		"model \"a\" {}\ninput x : number\ndecision d : number = x\n",
		"model \"b\" {}\ninput x : number\ndecision d : number = x + 1\n",
		"model \"c\" {}\ninput x : number\ndecision d : number = x + 2\n",
	} {
		if _, _, _, err := c.Compile([]byte(s)); err != nil {
			t.Fatalf("compile %d: %v", i, err)
		}
	}
	if len(c.items) > 2 {
		t.Errorf("cache exceeded its bound: %d entries", len(c.items))
	}
}
