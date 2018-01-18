//    Copyright 2018 Google Inc.
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

package server

import (
	"html/template"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"go.opencensus.io/trace"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
)

// Traces handles debug requests calls.
// Inspired heavily by golang.org/x/net/trace/trace.go
func Traces(w http.ResponseWriter, req *http.Request) {
	if !isAllowed(req) {
		http.Error(w, "Unauthorized.", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	data := &traceDisplay{
		Summary: trace.SampledSpansSummary(),
	}
	if req.FormValue("fam") != "" {
		data.DisplayFamily = req.FormValue("fam")
		bucket, err := strconv.Atoi(req.FormValue("b"))
		if err != nil {
			glog.Warningf("trace debug failed parse arg: $v", err)
			return
		}
		if bucket == -1 {
			data.MinLatency = 0
			data.Spans = trace.ActiveSpans(data.DisplayFamily)
		} else {
			methodSummary := data.Summary[data.DisplayFamily]
			bs := methodSummary.LatencyBuckets[bucket]
			data.MinLatency = bs.MinLatency
			data.Spans = trace.LatencySampledSpans(data.DisplayFamily, bs.MinLatency, bs.MaxLatency)
		}
	}

	glog.V(2).Infof("data: %v", data)

	if err := traceTmpl().ExecuteTemplate(w, "trace", data); err != nil {
		glog.Warningf("trace debug failed execute template: %v", err)
	}
}

type traceDisplay struct {
	Summary map[string]trace.PerMethodSummary

	DisplayFamily string
	MinLatency    time.Duration
	Spans         []*trace.SpanData
}

func isAllowed(r *http.Request) bool {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		h = r.RemoteAddr
	}
	switch h {
	case "localhost", "::1", "127.0.0.1":
		return true
	default:
		return false
	}
}

var traceTmplCache *template.Template
var traceTemplateOnce sync.Once

func traceTmpl() *template.Template {
	traceTemplateOnce.Do(func() {
		traceTmplCache = template.Must(template.New("trace").Funcs(template.FuncMap{
			"sdump": spew.Sdump,
			"tsub": func(a, b time.Time) time.Duration {
				return a.Sub(b)
			},
			"tdelta": func(idx int, span *trace.SpanData) time.Duration {
				ann := span.Annotations[idx]
				if idx == 0 {
					return ann.Time.Sub(span.StartTime)
				}
				return ann.Time.Sub(span.Annotations[idx-1].Time)
			},
		}).Parse(traceHTML))
	})
	return traceTmplCache
}

const traceHTML = `
<html>
	<head>
		<title>ddebug/requests</title>
		<style>
			.trace-data {
				font-family: monospace;
			}
		</style>
	</head>
	<body>
		<table>
		{{ range $fam, $summary := .Summary }}
			<tr>
				<td>
				{{$fam}}
				</td>
				<td>
					<a href="?fam={{$fam}}&b=-1">
					{{ $summary.Active }} active
					</a>
				</td>
				{{ range $i, $lb := $summary.LatencyBuckets }}
					<td>
						<a href="?fam={{$fam}}&b={{$i}}">
						[{{ $lb.MinLatency }} - {{ $lb.MaxLatency }} ({{$lb.Size}})]
						</a>
					</td>
				{{ end }}
			</tr>
		{{ end }}
		</table>
		{{ if .DisplayFamily }}
			<h2>{{.DisplayFamily}} {{.MinLatency}}</h2>
			{{ range $span := .Spans }}
				<p class="trace-data">
					<b>{{ $span.Name }}</b>. {{ tsub $span.EndTime $span.StartTime }} TraceID: {{$span.TraceID}} SpanID: {{$span.SpanID}} ParentSpanID: {{$span.ParentSpanID}}<br>
					{{ $span.Attributes }} <br>
					<table>
					<tr>
						<td>{{ $span.StartTime }}</td> <td style="width:30px;padding-left:10px">0ms</td> <td>Start</td>
					</tr>
					{{ range $idx, $ann := $span.Annotations }}
						<tr>
							<td>{{ $ann.Time }}</td> <td>{{ tdelta $idx $span }}</td> <td>{{ $ann.Message }}</td> <td>{{ $ann.Attributes }}</td>
						</tr>
					{{ end }}
					<tr>
						<td>{{ $span.EndTime }}</td> <td>{{ tsub $span.EndTime $span.StartTime }}</td> <td>Finish with Status {{ $span.Status.Code }}. {{ $span.Status.Message }}</td>
					</tr>
					</table>
				</p>

			{{ end }}
		{{ end }}
	</body>
</html>
`
