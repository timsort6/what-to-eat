package utils

func RemoveDuplicates[S ~[]E, E comparable](s S) S {
	set := map[E]bool{}
	arr := make([]E, 0)
	for _, v := range s {
		if !set[v] {
			set[v] = true
			arr = append(arr, v)
		}
	}

	return arr
}

func Intersect[S ~[]E, E comparable](a, b S) S {
	m := make(map[E]bool)
	for _, v := range a {
		m[v] = true
	}

	set := make(map[E]bool)
	res := make([]E, 0)
	for _, v := range b {
		if m[v] && !set[v] {
			res = append(res, v)
			set[v] = true
		}
	}
	return res
}
