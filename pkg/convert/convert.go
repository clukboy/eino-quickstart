package convert

func Float64ToFloat32(vecs [][]float64) [][]float32 {
	result := make([][]float32, len(vecs))
	for i, v := range vecs {
		f32 := make([]float32, len(v))
		for j, val := range v {
			f32[j] = float32(val)
		}
		result[i] = f32
	}
	return result
}

func Float32ToFloat64(vecs [][]float32) [][]float64 {
	result := make([][]float64, len(vecs))
	for i, v := range vecs {
		f64 := make([]float64, len(v))
		for j, val := range v {
			f64[j] = float64(val)
		}
		result[i] = f64
	}
	return result
}

func Uint64ToInt64(ids []uint64) []int64 {
	result := make([]int64, len(ids))
	for i, id := range ids {
		result[i] = int64(id)
	}
	return result
}

func Int64ToUint64(ids []int64) []uint64 {
	result := make([]uint64, len(ids))
	for i, id := range ids {
		result[i] = uint64(id)
	}
	return result
}
