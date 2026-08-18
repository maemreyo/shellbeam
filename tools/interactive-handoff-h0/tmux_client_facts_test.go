package main

import "testing"

func TestParseClientFactsAllowsControlModeWithoutHeight(t *testing.T) {
	got, err := parseClientFactsLine("client-20305||20305|0|attached,focused,control-mode,ignore-size,UTF-8|80|")
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != 20305 || got.Width != 80 || got.Height != 0 {
		t.Fatalf("facts=%#v", got)
	}
}

func TestParseClientFactsRejectsDisplayClientWithoutHeight(t *testing.T) {
	if _, err := parseClientFactsLine("client-7|/dev/ttys001|7|0|attached,focused,UTF-8|80|"); err == nil {
		t.Fatal("display client without height accepted")
	}
}
