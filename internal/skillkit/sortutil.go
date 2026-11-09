package skillkit

import "slices"

func sortStable[T any](s []T, cmp func(a, b T) int) { slices.SortStableFunc(s, cmp) }
