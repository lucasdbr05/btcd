package distribution

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestDistributionConsistency(t *testing.T) {
	k := 1000
	dist := NewRobustSoliton(k, 0.1, 0.05)
	rng := rand.New(rand.NewPCG(42, 0))
	expected := dist.ExpectedDegree()

	var sum float64
	for range 100000 {
		sum += float64(dist.SampleDegree(rng))
	}
	actual := sum / 100000
	if math.Abs(actual-expected) >= 0.1 {
		t.Fatalf("sampled average %f deviated from expected %f", actual, expected)
	}
}

func TestCDFIsValid(t *testing.T) {
	k := 1000
	dist := NewRobustSoliton(k, 0.1, 0.05)
	if dist.cdf[0] != 0 || dist.cdf[k] != 1 {
		t.Fatalf("bad endpoints: cdf[0]=%f cdf[k]=%f", dist.cdf[0], dist.cdf[k])
	}
	for d := 1; d <= k; d++ {
		if dist.cdf[d] < dist.cdf[d-1] {
			t.Fatalf("CDF not monotone at d=%d", d)
		}
	}
}

func TestPMFSumsToOne(t *testing.T) {
	k := 500
	dist := NewRobustSoliton(k, 0.1, 0.05)
	var total float64
	for d := 1; d <= k; d++ {
		total += dist.cdf[d] - dist.cdf[d-1]
	}
	if math.Abs(total-1) >= 1e-10 {
		t.Fatalf("PMF does not sum to 1: %f", total)
	}
}

func TestSamplingProducesValidDegrees(t *testing.T) {
	k := 100
	dist := NewRobustSoliton(k, 0.1, 0.05)
	rng := rand.New(rand.NewPCG(42, 0))
	for range 10000 {
		d := dist.SampleDegree(rng)
		if d < 1 || d > k {
			t.Fatalf("degree %d out of range", d)
		}
	}
}

func TestExpectedDegree(t *testing.T) {
	k := 1000
	dist := NewRobustSoliton(k, 0.1, 0.05)
	var expected float64
	for d := 1; d <= k; d++ {
		expected += float64(d) * (dist.cdf[d] - dist.cdf[d-1])
	}
	if math.Abs(dist.ExpectedDegree()-expected) >= 1e-10 {
		t.Fatalf("expected degree mismatch")
	}
}
