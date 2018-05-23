package sybil

// {{{ HIST COMPAT WRAPPER FOR BASIC HIST

type HistCompat struct {
	*BasicHist
}

func (hc *HistCompat) Min() int64 {

	return hc.BasicHist.Min
}

func (hc *HistCompat) Max() int64 {
	return hc.BasicHist.Max
}

func (hc *HistCompat) NewHist() Histogram {
	return hc.table.NewHist(&hc.Info)
}

func (h *HistCompat) Mean() float64 {
	return h.Avg
}

func (h *HistCompat) GetMeanVariance() float64 {
	return h.GetVariance() / float64(h.Count)
}

func (h *HistCompat) TotalCount() int64 {
	return h.Count
}

func (h *HistCompat) StdDev() float64 {
	return h.GetStdDev()
}

func (h *HistCompat) GetIntBuckets() map[int64]int64 {
	return h.GetSparseBuckets()
}

func (h *HistCompat) Range() (int64, int64) {
	return h.Info.Min, h.Info.Max
}

// }}}

// {{{ HIST COMPAT WRAPPER FOR MULTI HIST

type MultiHistCompat struct {
	*MultiHist

	Histogram *MultiHist
}

func (hc *MultiHistCompat) Min() int64 {

	return hc.Histogram.Min
}

func (hc *MultiHistCompat) Max() int64 {
	return hc.Histogram.Max
}

func (hc *MultiHistCompat) NewHist() Histogram {
	return newMultiHist(hc.table, hc.Info)
}

func (h *MultiHistCompat) Mean() float64 {
	return h.Avg
}

func (h *MultiHistCompat) GetMeanVariance() float64 {
	return h.GetVariance() / float64(h.Count)
}

func (h *MultiHistCompat) TotalCount() int64 {
	return h.Count
}

func (h *MultiHistCompat) StdDev() float64 {
	return h.GetStdDev()
}

func (h *MultiHistCompat) GetIntBuckets() map[int64]int64 {
	return h.GetSparseBuckets()
}

func (h *MultiHistCompat) Range() (int64, int64) {
	return h.Info.Min, h.Info.Max
}

// }}}
