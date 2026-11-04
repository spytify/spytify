package core

import "math"

func pow10(x float64) float64  { return math.Pow(10, x) }
func sqrt(x float64) float64   { return math.Sqrt(x) }
func round1(x float64) float64 { return math.Round(x*10) / 10 }

func maxInt(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
