package transcription

import "testing"

func TestNoiseGate(t *testing.T) {
	quiet := make([]float32, 16000)
	for i := range quiet {
		quiet[i] = 0.001
	}
	gateAudio(quiet, -45)
	for _, s := range quiet {
		if s != 0 {
			t.Fatal("quiet noise passed gate")
		}
	}
	speech := make([]float32, 16000)
	for i := 1000; i < 1320; i++ {
		speech[i] = 0.1
	}
	gateAudio(speech, -45)
	if speech[1100] != 0.1 {
		t.Fatal("brief speech suppressed")
	}
	disabled := []float32{0.001}
	gateAudio(disabled, 0)
	if disabled[0] != 0.001 {
		t.Fatal("disabled gate modified audio")
	}
	gateAudio(nil, -45)
}
