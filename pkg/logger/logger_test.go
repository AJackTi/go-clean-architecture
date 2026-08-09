package logger

import "testing"

func TestInitValidatesLevel(t *testing.T) {
	if err := Init("definitely-not-a-level"); err == nil {
		t.Fatal("Init accepted an invalid log level")
	}
	if err := Init(" ERROR "); err != nil {
		t.Fatalf("Init(valid level) = %v", err)
	}
	Info("logger test")
	_ = Sync()
}
