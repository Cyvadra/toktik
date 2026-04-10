package service

func sliceOrEmpty[T any](items []T) []T {
	if items == nil {
		return make([]T, 0)
	}
	return items
}
