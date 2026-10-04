package config

import "testing"

func TestOMVPort(t *testing.T) {
	cases := map[string]int{
		"http://127.0.0.1":        80,
		"http://127.0.0.1:81":     81,
		"https://omv.local":       443,
		"https://omv.local:8443/": 8443,
		"://broken":               80,
		"http://127.0.0.1:99999":  80,
	}
	for in, want := range cases {
		if got := (Config{OMVURL: in}).OMVPort(); got != want {
			t.Errorf("%q: got %d want %d", in, got, want)
		}
	}
}
