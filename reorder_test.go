package main

import (
	"flag"
	"reflect"
	"testing"
)

func TestReorderFlags(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	var platform string
	var output string
	var allTags, quiet bool
	fs.StringVar(&platform, "platform", "", "")
	fs.StringVar(&output, "o", "", "")
	fs.BoolVar(&allTags, "a", false, "")
	fs.BoolVar(&quiet, "quiet", false, "")
	fs.BoolVar(&quiet, "q", false, "")
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"flag 前置", []string{"-a"}, []string{"-a"}},
		{"flag 后置 value", []string{"alpine:3.19", "--platform", "linux/arm64", "-o", "out.tar"}, []string{"--platform", "linux/arm64", "-o", "out.tar", "alpine:3.19"}},
		{"flag 等号", []string{"nginx", "--platform=linux/arm64"}, []string{"--platform=linux/arm64", "nginx"}},
		{"bool 后置", []string{"nginx", "-q"}, []string{"-q", "nginx"}},
		{"混合", []string{"redis", "-a", "--platform", "linux/amd64"}, []string{"-a", "--platform", "linux/amd64", "redis"}},
	}
	for _, c := range cases {
		got, err := reorderFlags(fs, c.in)
		if err != nil {
			t.Fatalf("%s: 意外错误: %v", c.name, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: 得到 %v, 期望 %v", c.name, got, c.want)
		}
	}
}
