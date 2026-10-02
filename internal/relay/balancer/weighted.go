package balancer

import (
	"sync/atomic"

	"github.com/lingyuins/octopus/internal/model"
)

// maxWeightedSequenceLen 限制 Smooth WRR 展开序列的长度上限（权重和/公约数之后的硬帽）。
// 防御性上限：权重来自数据库，可能出现 sum 很大（如 20 个渠道每个 weight=100 → sum=2000）。
// 超过时压缩权重比例（等比例缩小）而非直接截断，保证分布语义仍正确。
const maxWeightedSequenceLen = 1000

// weightedRoundRobinCounter 全局轮转计数器：消费方顺序遍历展开序列，跨请求推进。
// roundRobinCounter 是包内既有计数器，语义一致（轮转起点），这里独立一个避免与
// RoundRobin 策略相互干扰计数节奏。
var weightedRoundRobinCounter uint64

// smoothWRRSequence 把 items 展开成平滑加权轮询序列（经典 Smooth Weighted
// Round-Robin，Nginx 同款）：每轮给每个候选累加自身 weight，选当前值最大者，
// 选中者减去总 weight。权重 5/1/1 展开为 A A B A C A A（周期 7，
// 高权重候选的穿插分布而不是吃满前 5 个位置）。
//
// 算法实现（interleaved 模式）：
//  1. 计算有效权重（weight<=0 按 1 处理）与权重和 totalWeight；
//  2. 若 sum 超过 maxWeightedSequenceLen，等比例缩小权重（scale down）直至
//     sum <= 上限或所有权重已到 1（此时退化为普通轮询序列）；
//  3. 经典 smooth WRR 展开生成完整周期序列（长度 = 权重和），
//     输出顺序即周期内平滑分布顺序。
//
// 输出保证：
//   - 序列长度 = 有效权重和（或压缩后的和），
//   - 每个 item 出现次数 = 其有效权重，
//   - 高权重 item 分散穿插在整个周期中，不会连续霸屏。
func smoothWRRSequence(items []model.GroupItem) []model.GroupItem {
	n := len(items)
	if n == 0 {
		return nil
	}

	// 1. 计算有效权重
	effective := make([]int, n)
	total := 0
	for i, item := range items {
		w := item.Weight
		if w <= 0 {
			w = 1
		}
		effective[i] = w
		total += w
	}

	// 2. 压缩过大的权重和：等比例缩小，无法再缩时（全 1）直接截断。
	if total > maxWeightedSequenceLen {
		total = compressWeights(effective, total)
	}

	// 3. 经典 smooth WRR：每轮所有 current += weight，选最大者输出并减去 total
	result := make([]model.GroupItem, 0, total)
	current := make([]int64, n)

	for len(result) < total {
		// 找当前值最大的候选（并列时先到先得，保持稳定）
		best := -1
		var bestVal int64
		for i := range effective {
			current[i] += int64(effective[i])
			if best < 0 || current[i] > bestVal {
				best = i
				bestVal = current[i]
			}
		}
		if best < 0 {
			break
		}
		current[best] -= int64(total)
		result = append(result, items[best])
	}

	return result
}

// compressWeights 等比例缩小权重直至 sum <= maxWeightedSequenceLen。
// 缩小到全部为 1 后仍未达标（渠道数本身 > 上限）时，截断到上限并返回实际 sum。
// 返回压缩后的总权重。
func compressWeights(weights []int, total int) int {
	for total > maxWeightedSequenceLen {
		// 找当前最大权重，若已全 1 则无法再缩
		maxW := 0
		for _, w := range weights {
			if w > maxW {
				maxW = w
			}
		}
		if maxW <= 1 {
			// 全 1：渠道数超上限，截断序列（此时语义退化为 round-robin）
			return maxWeightedSequenceLen
		}
		// 等比例缩小：每轮把 >1 的权重对半缩，保持相对比例
		for i := range weights {
			if weights[i] > 1 {
				weights[i] = (weights[i] + 1) / 2 // ceil 半缩，保持 >=1
			}
		}
		total = 0
		for _, w := range weights {
			total += w
		}
	}
	return total
}

// weightedCandidates 把 smooth WRR 序列从全局轮转位置开始排列。
// 调用方顺序遍历返回序列即自然消费展开序列的轮转窗口：
//   - counter 每次调用推进一个位置（与 RoundRobin 同节奏）；
//   - 从 counter 处开始取 total 个元素（环绕取，序列作为环形缓冲使用），
//     顺序遍历正好消费一个完整周期，各渠道出现次数精确等于其权重；
//   - 失败重试（NewIterator 的黑名单过滤、sticky、disposable 前置）由
//     iterator 层照常处理，不受影响。
//
// 返回完整展开序列（同一 item 可出现多次，长度 = 权重和）；消费方
// iterator.go 顺序遍历，重复槽位即「该渠道在本周期内的多次出场」，正是加权语义。
func weightedCandidates(items []model.GroupItem) []model.GroupItem {
	sequence := smoothWRRSequence(items)
	if len(sequence) == 0 {
		return nil
	}
	n := len(sequence)
	offset := int(atomic.AddUint64(&weightedRoundRobinCounter, 1) % uint64(n))
	result := make([]model.GroupItem, n)
	for i := 0; i < n; i++ {
		result[i] = sequence[(offset+i)%n]
	}
	return result
}
