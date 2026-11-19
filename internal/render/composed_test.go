package render

import "testing"

func TestComposed(t *testing.T) {
	for in, want := range map[string]string{
		`<p><b>Hi</b> <a href="https://example.org" onclick="x()">there</a></p>`: `<p><b>Hi</b> <a href="https://example.org">there</a></p>`,
		`<div data-signature=""><p>Al</p></div><script>x()</script>`:             `<div data-signature=""><p>Al</p></div>`,
		`<img src="cid:a1" alt="map"><img src="https://tracker.example/p.gif">`:  `<img src="cid:a1" alt="map"><img src="">`,
		`<p style="color:red" class="x">t</p><table><tr><td>c</td></tr></table>`: `<p>t</p>c`,
	} {
		if got := Composed(in); got != want {
			t.Errorf("Composed(%s)\n got  %s\n want %s", in, got, want)
		}
	}
}
