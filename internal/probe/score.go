package probe

import "ipscan/internal/model"

// evaluate turns the measured numbers into a 1-100 score and a grade. The
// weights are deliberately simple and monotonic: latency dominates, then
// jitter, then loss, with a small penalty when a TLS handshake could not be
// completed at all.
func evaluate(res *Result, n model.Node) {
	score := 100

	switch ms := res.Avg.Milliseconds(); {
	case ms <= 80:
	case ms <= 150:
		score -= 5
	case ms <= 250:
		score -= 12
	case ms <= 400:
		score -= 22
	case ms <= 700:
		score -= 35
	default:
		score -= 50
	}

	switch j := res.Jitter.Milliseconds(); {
	case j <= 10:
	case j <= 30:
		score -= 5
	case j <= 80:
		score -= 12
	default:
		score -= 20
	}

	score -= int(res.Loss*45 + 0.5)

	if res.TLS != nil && res.TLS.Error != "" && (n.Security == "tls" || n.Security == "reality") {
		score -= 10
	}

	res.Score = clamp(score, 1, 100)
	res.Grade = GradeFromScore(res.Score, true)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
