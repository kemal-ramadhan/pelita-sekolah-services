package controller

import (
	"strings"
	"testing"
)

func TestSafeFileName(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":         "passwd",
		`C:\Users\x\kk "scan".pdf`: "kk scan.pdf",
		"a\r\nX-Evil: 1.pdf":       "aX-Evil: 1.pdf",
		"":                         "dokumen",
	}
	for in, want := range cases {
		if got := safeFileName(in); got != want {
			t.Errorf("safeFileName(%q) = %q want %q", in, got, want)
		}
	}
}

func TestContentDisposition(t *testing.T) {
	v := contentDisposition("Akta \"lahir\"\r\n.pdf")
	if strings.ContainsAny(v, "\r\n") || !strings.HasPrefix(v, "attachment;") {
		t.Fatalf("unsafe header %q", v)
	}
	if v := contentDisposition("Kartu Keluarga é.pdf"); !strings.Contains(v, "filename*=") {
		t.Fatalf("non-ASCII must use RFC 2231: %q", v)
	}
}
