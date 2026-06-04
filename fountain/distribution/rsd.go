package distribution

import (
	"fmt"
	"math"
	"math/rand/v2"
)

type DegreeDistribution interface {
	SampleDegree(rng *rand.Rand) int
	ExpectedDegree() float64
}

type RobustSoliton struct {
	C     float64
	Delta float64
	k     int
	cdf   []float64
}

func NewRobustSoliton(k int, c, delta float64) *RobustSoliton {
	if k <= 0 {
		panic("k must be > 0")
	}
	if c <= 0 {
		panic("c must be > 0")
	}
	if delta <= 0 || delta >= 1 {
		panic("delta must be in (0, 1)")
	}
	cdf := buildCDF(k, c, delta)
	return &RobustSoliton{C: c, Delta: delta, k: k, cdf: cdf}
}

func (r *RobustSoliton) Rebuild(k int) {
	if k <= 0 {
		panic("k must be > 0")
	}
	if r.k == k {
		return
	}
	r.cdf = buildCDF(k, r.C, r.Delta)
	r.k = k
}

func (r *RobustSoliton) SampleDegree(rng *rand.Rand) int {
	u := rng.Float64()
	lo, hi := 0, len(r.cdf)-2
	for lo < hi {
		mid := (lo + hi) / 2
		if r.cdf[mid+1] < u {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	d := lo + 1
	if d < 1 {
		d = 1
	}
	if d > r.k {
		d = r.k
	}
	return d
}

func (r *RobustSoliton) ExpectedDegree() float64 {
	var total float64
	for d := 1; d <= r.k; d++ {
		p := r.cdf[d] - r.cdf[d-1]
		total += float64(d) * p
	}
	return total
}

func (r *RobustSoliton) String() string {
	return fmt.Sprintf("RobustSoliton{k=%d, c=%.3f, delta=%.3f, E[D]=%.3f}",
		r.k, r.C, r.Delta, r.ExpectedDegree())
}

func buildCDF(k int, c, delta float64) []float64 {
	kf := float64(k)
	s := c * math.Log(kf/delta) * math.Sqrt(kf)
	rBoundary := int(math.Max(math.Floor(kf/s), 1))

	pmf := make([]float64, k+1)
	var betaSum float64

	for d := 1; d <= k; d++ {
		df := float64(d)
		var rho float64
		if d == 1 {
			rho = 1.0 / kf
		} else {
			rho = 1.0 / (df * (df - 1))
		}
		var tau float64
		if rBoundary > 0 {
			switch {
			case d < rBoundary:
				tau = s / (df * kf)
			case d == rBoundary:
				tau = (s / kf) * math.Log(s/delta)
			}
		}
		pmf[d] = rho + tau
		betaSum += pmf[d]
	}

	cdf := make([]float64, k+1)
	var running float64
	for d := 1; d <= k; d++ {
		running += pmf[d] / betaSum
		cdf[d] = running
	}
	cdf[k] = 1.0
	return cdf
}

func (r *RobustSoliton) K() int { return r.k }
