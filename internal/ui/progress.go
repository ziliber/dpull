// Package ui 提供面向终端的拉取进度显示，风格模仿 `docker pull`。
package ui

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/containers/image/v5/types"
	digest "github.com/opencontainers/go-digest"
	"golang.org/x/term"
)

// layerState 记录单个 layer（blob）的下载状态。
type layerState struct {
	short    string // 短 digest（前 12 位），同 docker pull
	status   rune   // 状态标识：' ' 下载中 / 'D' 完成 / 'E' 抽取 / 'A' 已存在
	current  uint64 // 已下载字节
	total    int64  // 总字节（-1 未知）
	finished bool
}

// Progress 收集 copy.Image 的进度事件并渲染为多行动态进度。
//
// 使用方法：
//
//	p := ui.NewProgress(os.Stdout, quiet, "")
//	ch := p.Channel()
//	defer p.Close()                    // copy.Image 返回后再调用，归位光标
//	copy.Image(..., &copy.Options{
//		Progress:         ch,
//		ProgressInterval: 100 * time.Millisecond,
//	})
//
// 当输出非终端或 quiet 为 true 时，Progress 静默（不输出进度）。
type Progress struct {
	out io.Writer

	mu     sync.Mutex
	layers map[digest.Digest]*layerState
	order  []digest.Digest // 首次出现的顺序
	active bool            // 当前是否正在展示动态进度

	ch      chan types.ProgressProperties // 即设备（双向），对外暴露 send-only
	once    sync.Once
	enabled bool // TTY 且非 quiet
}

// NewProgress 创建一个进度渲染器。out 通常为 os.Stdout。
// quiet 为 true 时不输出任何进度。
func NewProgress(out io.Writer, quiet bool) *Progress {
	return &Progress{
		out:     out,
		layers:  make(map[digest.Digest]*layerState),
		enabled: isTerminal(out) && !quiet,
	}
}

// isTerminal 报告 w 是否连接到终端。
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// Enabled 返回是否处于动态进度模式（TTY 且非 quiet）。
// 非动态模式时调用方可决定是否走备用输出。
func (p *Progress) Enabled() bool {
	return p.enabled
}

// Channel 返回可传给 copy.Options.Progress 的通道（双向，内部交由事件循环消费）。
// 调用方将事件喂给该通道；复制结束后必须调用 Close。
func (p *Progress) Channel() chan types.ProgressProperties {
	if p.ch == nil {
		ch := make(chan types.ProgressProperties, 16)
		p.ch = ch
		go p.loop(ch)
	}
	return p.ch
}

// closeChan 关闭内部通道，触发事件循环收尾。幂等。
func (p *Progress) closeChan() {
	if p.ch != nil {
		close(p.ch)
	}
}

// loop 消费进度事件并渲染。
func (p *Progress) loop(ch <-chan types.ProgressProperties) {
	for e := range ch {
		p.handle(e)
		if p.enabled {
			p.render()
		}
	}
}

// handle 更新单个 layer 状态。
func (p *Progress) handle(e types.ProgressProperties) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = true
	ls, ok := p.layers[e.Artifact.Digest]
	if !ok {
		ls = &layerState{short: shortDigest(e.Artifact.Digest), total: e.Artifact.Size}
		p.layers[e.Artifact.Digest] = ls
		p.order = append(p.order, e.Artifact.Digest)
	}
	switch e.Event {
	case types.ProgressEventNewArtifact:
		ls.status = ' '
		ls.current = 0
		ls.finished = false
	case types.ProgressEventRead:
		ls.current = e.Offset
	case types.ProgressEventDone:
		ls.current = e.Offset
		ls.finished = true
		if e.Artifact.Size > 0 && e.Offset >= uint64(e.Artifact.Size) {
			ls.status = 'D'
		}
	case types.ProgressEventSkipped:
		ls.finished = true
		ls.status = 'A' // Already exists
	}
}

// Close 结束渲染：关闭事件循环，输出每个 layer 的最终状态。
// 必须在 copy.Image 返回后再调用（否则可能向已关闭通道发送而 panic）。
// 可安全多次调用。
func (p *Progress) Close() {
	p.once.Do(func() {
		p.closeChan()
		p.finish()
	})
}

// render 将当前所有 layer 状态以动画方式重绘到屏幕上。
func (p *Progress) render() {
	p.mu.Lock()
	lines := p.lines()
	p.mu.Unlock()
	p.paint(lines)
}

// finish 输出最终结果行并归位（清除动画）。非动态或无状态时静默。
func (p *Progress) finish() {
	if !p.enabled {
		return
	}
	p.mu.Lock()
	if !p.active {
		p.mu.Unlock()
		return
	}
	lines := p.lines()
	p.active = false
	p.mu.Unlock()
	p.paint(lines)
}

// paint 将 lines 以覆盖方式写到屏幕上：光标先回到所有行之上，再逐行写入。
func (p *Progress) paint(lines []string) {
	if len(lines) == 0 {
		return
	}
	// 先向上回退到已渲染区域顶部
	fmt.Fprint(p.out, "\r")
	for range lines {
		fmt.Fprint(p.out, "\033[1A\x1b[K")
	}
	fmt.Fprint(p.out, "\r")
	for _, l := range lines {
		fmt.Fprintln(p.out, "\x1b[K"+l)
	}
}

// lines 返回按 digest 排序的每行状态文本。调用方需持有 p.mu。
func (p *Progress) lines() []string {
	order := make([]digest.Digest, len(p.order))
	copy(order, p.order)
	sort.Slice(order, func(i, j int) bool {
		return p.layers[order[i]].short < p.layers[order[j]].short
	})
	out := make([]string, 0, len(order))
	for _, d := range order {
		out = append(out, layerLine(p.layers[d]))
	}
	return out
}

// layerLine 生成单行 layer 状态文本，样式同 docker pull。
func layerLine(ls *layerState) string {
	switch ls.status {
	case 'A':
		return fmt.Sprintf("%s: Already exists", ls.short)
	case 'D':
		return fmt.Sprintf("%s: Download complete", ls.short)
	case 'E':
		return fmt.Sprintf("%s: Extracting", ls.short)
	}
	// 下载中：显示百分比或已下载量
	if ls.total > 0 {
		pct := float64(ls.current) / float64(ls.total) * 100
		return fmt.Sprintf("%s: Downloading %6.2f%%", ls.short, pct)
	}
	return fmt.Sprintf("%s: Downloading %s", ls.short, fmtBytes(int64(ls.current)))
}

// shortDigest 取 digest 后 12 位十六进制（去掉前缀如 "sha256:"）。
func shortDigest(d digest.Digest) string {
	s := string(d)
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[i+1:]
	}
	const n = 12
	if len(s) > n {
		s = s[:n]
	}
	return s
}

// fmtBytes 格式化字节数为人类可读。
func fmtBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2fGB", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.2fMB", float64(b)/MB)
	case b >= KB:
		return fmt.Sprintf("%.2fKB", float64(b)/KB)
	default:
		return fmt.Sprintf("%dB", b)
	}
}
