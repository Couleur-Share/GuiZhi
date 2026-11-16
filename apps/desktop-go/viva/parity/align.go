package parity

// alignKeys 用最长公共子序列把两个键序列对齐：返回成对的序号，以及只在 a、只在 b 里的序号。
// 节点序列一般几十到几百项，O(n·m) 足够。
func alignKeys(a, b []string) (pairs [][2]int, onlyA, onlyB []int) {
	n, m := len(a), len(b)
	// lcs[i][j] 是 a[i:] 与 b[j:] 的最长公共子序列长度
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			pairs = append(pairs, [2]int{i, j})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			onlyA = append(onlyA, i)
			i++
		default:
			onlyB = append(onlyB, j)
			j++
		}
	}
	for ; i < n; i++ {
		onlyA = append(onlyA, i)
	}
	for ; j < m; j++ {
		onlyB = append(onlyB, j)
	}
	return pairs, onlyA, onlyB
}
