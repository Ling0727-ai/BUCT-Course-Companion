package main

import (
	"encoding/json"
	"io"
	"os"
	"time"

	"buct-course-local/worker/internal/course"
	"buct-course-local/worker/internal/courseflow"
)

func main() {
	output := os.Stdout
	os.Stdout = os.Stderr // The upstream client writes diagnostics to stdout.
	time.Local = time.FixedZone("Asia/Shanghai", 8*60*60)
	if len(os.Args) == 2 && os.Args[1] == "schedule" {
		var request courseflow.Request
		if err := json.NewDecoder(io.LimitReader(os.Stdin, 3<<20)).Decode(&request); err != nil {
			_ = json.NewEncoder(output).Encode(courseflow.Reply{Error: "无法读取课表请求。"})
			return
		}
		_ = json.NewEncoder(output).Encode(courseflow.Run(request, time.Now()))
		return
	}

	var request course.Request
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 1<<20)).Decode(&request); err != nil {
		_ = json.NewEncoder(output).Encode(course.Result{Error: "课程同步请求格式无效。"})
		return
	}
	_ = json.NewEncoder(output).Encode(course.Run(request))
}
