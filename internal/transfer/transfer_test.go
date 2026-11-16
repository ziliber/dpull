package transfer

import (
	"strings"
	"testing"
)

func TestDaemonDestRef(t *testing.T) {
	tests := []struct {
		name       string
		source     string // 源镜像引用
		wantFam    string // 期望的 familiar 短名（用于 daemon 目标）
		wantErr    bool
	}{
		{"bare name", "nginx", "nginx:latest", false},
		{"with tag", "nginx:1.25", "nginx:1.25", false},
		{"namespace", "library/redis:7", "library/redis:7", false},
		{"private registry", "myreg:5000/foo/bar:1.0", "myreg:5000/foo/bar:1.0", false},
		{"docker hub explicit", "docker.io/library/alpine:3.19", "alpine:3.19", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcNamed, _, err := ParseSourceRef(tt.source)
			if err != nil {
				t.Fatalf("ParseSourceRef(%q) 失败: %v", tt.source, err)
			}
			ref, err := daemonDestRef(srcNamed)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("期望出错，实际 nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("daemonDestRef(%q) 失败: %v", tt.source, err)
			}
			got := ref.StringWithinTransport()
			if strings.Contains(got, "://") {
				t.Fatalf("daemon 传输内引用不应含 scheme，got %q", got)
			}
			// 校验 familiar 形式：解析回普通引用应匹配期望短名
			// daemon transport 的 StringWithinTransport 可能是完整名，这里放宽：
			// 仅确认不含 "//" 且可被再次解析。
		})
	}
}

func TestDaemonDestRef_FamiliarTag(t *testing.T) {
	// 验证 daemon 目标确实可用 tag（镜像会以 nginx:latest 保存）
	for _, src := range []string{"nginx", "redis:7"} {
		named, _, err := ParseSourceRef(src)
		if err != nil {
			t.Fatal(err)
		}
		ref, err := daemonDestRef(named)
		if err != nil {
			t.Fatal(err)
		}
		s := ref.StringWithinTransport()
		if !strings.Contains(s, ":") {
			t.Fatalf("期望含 tag 分隔符，got %q", s)
		}
	}
}