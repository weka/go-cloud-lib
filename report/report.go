package report

import (
	"fmt"
	"github.com/weka/go-cloud-lib/protocol"
	"time"
)

func UpdateReport(report protocol.Report, state *protocol.ClusterState) (err error) {
	currentTime := time.Now().UTC().Format("15:04:05") + " UTC"
	switch report.Type {
	case "error":
		if state.Errors == nil {
			state.Errors = make(map[string][]string)
		}
		state.Errors[report.Hostname] = append(state.Errors[report.Hostname], fmt.Sprintf("%s: %s", currentTime, report.Message))
	case "progress":
		if state.Progress == nil {
			state.Progress = make(map[string][]string)
		}
		state.Progress[report.Hostname] = append(state.Progress[report.Hostname], fmt.Sprintf("%s: %s", currentTime, report.Message))
	case "debug":
		if state.Debug == nil {
			state.Debug = make(map[string][]string)
		}
		state.Debug[report.Hostname] = append(state.Debug[report.Hostname], fmt.Sprintf("%s: %s", currentTime, report.Message))
	default:
		err = fmt.Errorf("invalid type: %s", report.Type)
		return
	}
	return
}
