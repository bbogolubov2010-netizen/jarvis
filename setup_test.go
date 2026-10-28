package main

import "testing"

func TestHasModelNormalizesLatestTag(t *testing.T) {
	models := []string{"llama3.1:8b:latest", "qwen2.5:14b"}
	if !hasModel(models, "llama3.1:8b") {
		t.Fatal("expected :latest suffix to be ignored")
	}
	if !hasModel(models, "qwen2.5:14b:latest") {
		t.Fatal("expected :latest suffix to be ignored on the query")
	}
	if hasModel(models, "qwen2.5:32b") {
		t.Fatal("unexpected model match")
	}
	if hasModel(models, "") {
		t.Fatal("an empty model name must not match")
	}
}

func TestModelNamesEqual(t *testing.T) {
	cases := []struct {
		left  string
		right string
		want  bool
	}{
		{"llama3.1:8b", "llama3.1:8b:latest", true},
		{"llama3.1:8b", "llama3.1:8b", true},
		{"llama3.1:8b", "llama3.2:3b", false},
	}
	for _, testCase := range cases {
		if got := modelNamesEqual(testCase.left, testCase.right); got != testCase.want {
			t.Fatalf("modelNamesEqual(%q, %q) = %v, want %v", testCase.left, testCase.right, got, testCase.want)
		}
	}
}

func TestFormatProgressBytes(t *testing.T) {
	cases := map[int64]string{
		0:       "0 B",
		512:     "512 B",
		1536:    "1.5 KB",
		2 << 20: "2.0 MB",
		3 << 30: "3.00 GB",
	}
	for input, want := range cases {
		if got := formatProgressBytes(input); got != want {
			t.Fatalf("formatProgressBytes(%d) = %q, want %q", input, got, want)
		}
	}
}
