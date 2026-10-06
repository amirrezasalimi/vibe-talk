package transcription

import "math"

// Gate whole chunks, never skip them: Whistle must still receive silence to
// advance its clock and confirm/flush previous speech. Short-window peaks
// protect brief words that a whole-chunk average would classify as quiet.
func gateAudio(samples []float32, floorDB float64) {
	if floorDB == 0 || len(samples) == 0 {
		return
	}
	threshold := math.Pow(10, floorDB/10)
	for start := 0; start < len(samples); start += 320 {
		end := min(start+320, len(samples))
		energy := 0.0
		for _, sample := range samples[start:end] {
			energy += float64(sample) * float64(sample)
		}
		if energy/float64(end-start) >= threshold {
			return
		}
	}
	clear(samples)
}
