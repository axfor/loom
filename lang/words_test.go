package lang

import "testing"

// Two names with the same words are one name written two ways: that is what lets base.fooBar
// and base.Foo_Bar both find "Foo Bar".
func TestWords(t *testing.T) {
	same := [][2]string{
		{"Foo Bar", "Foo_Bar"}, {"Foo Bar", "fooBar"}, {"Foo Bar", "FooBar"}, {"Foo Bar", "foo_bar"},
		{"foo-bar", "fooBar"}, {"How it compares", "How_it_compares"}, {"HTTP Server", "HTTPServer"},
		{"Step 1", "Step_1"}, {"v2 Setup", "v2Setup"}, {"已有中文", "已有中文"}, {"a  b", "a_b"},
	}
	for _, p := range same {
		if Words(p[0]) != Words(p[1]) {
			t.Errorf("%q and %q are the same words: %q vs %q", p[0], p[1], Words(p[0]), Words(p[1]))
		}
	}
	differ := [][2]string{
		{"Foo Bar", "Foobar"}, {"How it compares?", "How_it_compares"}, {"Step 1", "Step1"}, {"ab", "a_b"},
	}
	for _, p := range differ {
		if Words(p[0]) == Words(p[1]) {
			t.Errorf("%q and %q are different words, both read %q", p[0], p[1], Words(p[0]))
		}
	}
}
