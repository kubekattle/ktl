package main

import "testing"

func TestRequestedHelpRecognizesDash(t *testing.T) {
	if !requestedHelp("-") {
		t.Fatalf("expected single dash to trigger help detection")
	}
}
