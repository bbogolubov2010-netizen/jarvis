package voiceinput

import "math"

const (
	sampleRate       = 16000
	frameSamples     = 480 // 30 ms
	preRollFrames    = 10  // 300 ms
	startFrames      = 3
	endSilenceFrames = 24  // 720 ms
	minSpeechFrames  = 10  // 300 ms
	maxSpeechFrames  = 600 // 18 s
)

// Segmenter is a small energy-based voice activity detector. It is deliberately
// conservative: false negatives are cheaper than transcribing background noise.
type Segmenter struct {
	noiseFloor float64
	threshold  float64

	inSpeech   bool
	startCount int
	silence    int
	preRoll    [][]int16
	current    []int16
	frames     int
}

// NewSegmenter creates a VAD with a reasonable default noise floor.
func NewSegmenter() *Segmenter {
	return &Segmenter{
		noiseFloor: 0.006,
		threshold:  0.018,
	}
}

// Feed consumes one 30 ms frame and returns a completed utterance when speech
// ends. The returned slice is a copy and is safe to keep.
func (s *Segmenter) Feed(frame []int16) [][]int16 {
	if len(frame) == 0 {
		return nil
	}
	rms := normalizedRMS(frame)
	s.updateNoise(rms)
	voice := rms > s.threshold

	if !s.inSpeech {
		s.preRoll = append(s.preRoll, clonePCM(frame))
		if len(s.preRoll) > preRollFrames {
			s.preRoll = s.preRoll[1:]
		}
		if voice {
			s.startCount++
		} else {
			s.startCount = 0
		}
		if s.startCount >= startFrames {
			s.inSpeech = true
			s.frames = 0
			s.silence = 0
			s.current = s.current[:0]
			for _, item := range s.preRoll {
				s.current = append(s.current, item...)
			}
			s.frames = len(s.preRoll)
			s.preRoll = s.preRoll[:0]
		}
		return nil
	}

	s.current = append(s.current, frame...)
	s.frames++
	if voice {
		s.silence = 0
	} else {
		s.silence++
	}

	if s.frames >= maxSpeechFrames || s.silence >= endSilenceFrames {
		return s.finish()
	}
	return nil
}

// Flush returns the current speech segment, if any.
func (s *Segmenter) Flush() [][]int16 {
	if !s.inSpeech {
		return nil
	}
	return s.finish()
}

// Reset clears all state.
func (s *Segmenter) Reset() {
	s.inSpeech = false
	s.startCount = 0
	s.silence = 0
	s.frames = 0
	s.current = s.current[:0]
	s.preRoll = s.preRoll[:0]
}

func (s *Segmenter) finish() [][]int16 {
	utterance := clonePCM(s.current)
	valid := s.frames >= minSpeechFrames && len(utterance) >= sampleRate/4
	s.inSpeech = false
	s.startCount = 0
	s.silence = 0
	s.frames = 0
	s.current = s.current[:0]
	if !valid {
		return nil
	}
	return [][]int16{utterance}
}

func (s *Segmenter) updateNoise(rms float64) {
	if s.inSpeech {
		return
	}
	// Track quiet frames quickly, loud frames slowly. This lets the detector
	// adapt to a noisy room without waking on steady fan noise.
	if rms < s.noiseFloor {
		s.noiseFloor = s.noiseFloor*0.92 + rms*0.08
	} else {
		s.noiseFloor = s.noiseFloor*0.995 + rms*0.005
	}
	if s.noiseFloor < 0.0015 {
		s.noiseFloor = 0.0015
	}
	s.threshold = math.Max(0.012, s.noiseFloor*2.8)
}

func normalizedRMS(frame []int16) float64 {
	if len(frame) == 0 {
		return 0
	}
	var sum float64
	for _, sample := range frame {
		v := float64(sample) / 32768.0
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(frame)))
}

func clonePCM(src []int16) []int16 {
	dst := make([]int16, len(src))
	copy(dst, src)
	return dst
}
