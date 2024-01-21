package gopls

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type (
	// A range of characters in a file
	Location struct {
		File     string
		Line     int
		startCol int
		endCol   int
	}

	symbol struct {
		name       string
		symbolType string
		location   Location
	}

	Function struct {
		Name     string
		Location Location
	}

	// Site(s) of a call to our focal function from within a function.
	CallSite struct {
		// In the case of caller sites, this is the Function def containing the caller sites.
		// In the case of callee sites, this is the Function def which they are calling.
		Function Function
		// Location(s) of call sites (there can be multiple calls to the focal
		// function within the same FnDef)
		Locations []Location
	}

	Node struct {
		Callers  []CallSite
		Function Function
		callees  []CallSite
	}
)

func CallHierarchy(position string) (Node, error) {
	fmt.Fprintf(os.Stderr, "CallHierarchy: %s\n", position)
	output, err := run("gopls", "call_hierarchy", position)
	if err != nil {
		return Node{}, err
	}
	node, err := parseCallHierarchyResponse(output)
	if err != nil {
		return Node{}, err
	}
	for i, c := range node.Callers {
		if enclosingName, isAnonymous := c.Function.isAnonymous(); isAnonymous {
			// gopls is unable to find incoming calls to an anonymous function.
			// Replace the anonymous function with its enclosing function and follow
			// edges from there.
			f, err := GetFunction(enclosingName, c.Function.Location.File)
			if err != nil {
				return Node{}, err
			}
			node.Callers[i].Function = f
		}
	}
	return node, nil
}

func GetFunction(name, file string) (Function, error) {
	fmt.Fprintf(os.Stderr, "GetFunction: %s %s\n", name, file)
	output, err := run("gopls", "symbols", file)
	if err != nil {
		return Function{}, err
	}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		symbol, err := parseSymbolsResponse(line, file)
		if err != nil {
			return Function{}, err
		}
		if (symbol.symbolType == "Function" || symbol.symbolType == "Method") && symbol.name == name {
			return Function{
				Name:     symbol.name,
				Location: symbol.location,
			}, nil
		}
	}
	return Function{}, fmt.Errorf("failed to locate symbol %s in file %s", name, file)
}

// $ gopls symbols service/history/api/reapplyevents/api.go
// Invoke Function 47:6-47:12
// $ gopls symbols /Users/dan/src/temporalio/temporal/service/history/transfer_queue_active_task_executor_test.go
// (*transferQueueActiveTaskExecutorSuite).TestPendingCloseExecutionTasks Method 2392:48-2392:78

func parseSymbolsResponse(line, file string) (symbol, error) {
	re := regexp.MustCompile(`(\w+) (\w+) (\d+):(\d+)-(\d+):(\d+)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) != 7 {
		return symbol{}, nil
	}
	lineNum := mustInt(matches[3])
	endLineNum := mustInt(matches[5])
	if endLineNum != lineNum {
		return symbol{}, fmt.Errorf(
			"start and end line numbers differ (%d vs %d) in response for symbols query: %s",
			lineNum,
			endLineNum,
			line,
		)
	}
	return symbol{
		name:       matches[1],
		symbolType: matches[2],
		location: Location{
			File:     file,
			Line:     lineNum,
			startCol: mustInt(matches[4]),
			endCol:   mustInt(matches[6]),
		},
	}, nil
}

// identifier: function reapplyEvents in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go:715:32-45
func parseFunctionDef(line string) (Function, error) {
	re := regexp.MustCompile(`function (\w+) in (\S+):(\d+):(\d+)-(\d+)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) != 6 {
		return Function{}, fmt.Errorf("unexpected format: %s", line)
	}
	return Function{
		Name: matches[1],
		Location: Location{
			File:     matches[2],
			Line:     mustInt(matches[3]),
			startCol: mustInt(matches[4]),
			endCol:   mustInt(matches[5]),
		},
	}, nil
}

func run(executable string, args ...string) (string, error) {
	cmd := exec.Command(executable, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

func (f Function) isAnonymous() (string, bool) {
	return strings.CutSuffix(f.Name, ".func()")
}

func (f Function) IsTest() bool {
	return strings.Contains(strings.ToLower(f.Name), "test")
}

func (f Function) LocationString() string {
	return f.Location.String()
}

func (r Location) String() string {
	return fmt.Sprintf("%s:%d:%d-%d", r.File, r.Line, r.startCol, r.endCol)
}

// $ gopls call_hierarchy ~/src/temporalio/temporal/service/history/ndc/workflow_resetter.go:715:32
const exampleResponse = `caller[0]: ranges 174:18-31 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function ResetWorkflow.func() in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go:157:21-25
caller[1]: ranges 333:11-24 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function reapplyEventsToResetWorkflow in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go:312:32-60
caller[2]: ranges 701:15-28 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function reapplyWorkflowEvents in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go:673:32-53
caller[3]: ranges 833:28-41 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter_test.go from/to function TestReapplyEvents in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter_test.go:790:33-50
identifier: function reapplyEvents in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go:715:32-45
callee[0]: ranges 734:18-67 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function GetWorkflowExecutionUpdateAcceptedEventAttributes in /Users/dan/src/temporalio/api-go/history/v1/message.pb.go:4856:24-73
callee[1]: ranges 741:18-75 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function GetWorkflowExecutionUpdateRequestReappliedEventAttributes in /Users/dan/src/temporalio/api-go/history/v1/message.pb.go:4898:24-81
callee[2]: ranges 743:10-20 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function GetRequest in /Users/dan/src/temporalio/api-go/history/v1/message.pb.go:4414:66-76
callee[3]: ranges 721:16-28 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function GetEventType in /Users/dan/src/temporalio/api-go/history/v1/message.pb.go:4541:24-36
callee[4]: ranges 726:10-18 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function GetInput in /Users/dan/src/temporalio/api-go/history/v1/message.pb.go:2307:52-60
callee[5]: ranges 723:18-61 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function GetWorkflowExecutionSignaledEventAttributes in /Users/dan/src/temporalio/api-go/history/v1/message.pb.go:4716:24-67
callee[6]: ranges 727:10-21 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function GetIdentity in /Users/dan/src/temporalio/api-go/history/v1/message.pb.go:2314:52-63
callee[7]: ranges 724:30-58 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function AddWorkflowExecutionSignaled in /Users/dan/src/temporalio/temporal/service/history/workflow/mutable_state.go:154:3-31
callee[8]: ranges 729:10-37 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function GetSkipGenerateWorkflowTask in /Users/dan/src/temporalio/api-go/history/v1/message.pb.go:2328:52-79
callee[9]: ranges 735:30-77, 742:30-77 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function AddWorkflowExecutionUpdateRequestReappliedEvent in /Users/dan/src/temporalio/temporal/service/history/workflow/mutable_state.go:162:3-50
callee[10]: ranges 736:10-28 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function GetAcceptedRequest in /Users/dan/src/temporalio/api-go/history/v1/message.pb.go:4214:58-76
callee[11]: ranges 725:10-23 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function GetSignalName in /Users/dan/src/temporalio/api-go/history/v1/message.pb.go:2300:52-65
callee[12]: ranges 728:10-19 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function GetHeader in /Users/dan/src/temporalio/api-go/history/v1/message.pb.go:2321:52-61`

func parseCallHierarchyResponse(response string) (Node, error) {
	parsed := Node{}
	for _, line := range strings.Split(strings.TrimSpace(response), "\n") {
		if strings.HasPrefix(line, "identifier") {
			def, err := parseFunctionDef(line)
			if err != nil {
				return parsed, err
			}
			parsed.Function = def
			continue
		}
		callSites, err := parseCallSite(line)
		if err != nil {
			return parsed, err
		}
		if strings.HasPrefix(line, "caller") {
			parsed.Callers = append(parsed.Callers, callSites)
		} else if strings.HasPrefix(line, "callee") {
			parsed.callees = append(parsed.callees, callSites)
		} else {
			return parsed, fmt.Errorf("unexpected line: %s", line)
		}
	}
	return parsed, nil
}

func parseCallSite(line string) (CallSite, error) {
	re := regexp.MustCompile(`calle[er]\[\d+\]: ranges (.+) in (.+) from/to function (.+) in (.+):(\d+):(\d+)-(\d+)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) != 8 {
		return CallSite{}, fmt.Errorf("unexpected format: '%s'", line)
	}

	var ranges []Location
	rangePattern := regexp.MustCompile(`(\d+):(\d+)-(\d+)`)
	rangeMatches := rangePattern.FindAllStringSubmatch(matches[1], -1)
	for _, match := range rangeMatches {
		line, _ := strconv.Atoi(match[1])
		startCol, _ := strconv.Atoi(match[2])
		endCol, _ := strconv.Atoi(match[3])
		ranges = append(ranges, Location{
			File:     matches[2],
			Line:     line,
			startCol: startCol,
			endCol:   endCol,
		})
	}

	function := Function{
		Name: matches[3],
		Location: Location{
			File:     matches[4],
			Line:     mustInt(matches[5]),
			startCol: mustInt(matches[6]),
			endCol:   mustInt(matches[7]),
		},
	}

	return CallSite{
		Function:  function,
		Locations: ranges,
	}, nil
}

func mustInt(a string) int {
	i, _ := strconv.Atoi(a)
	return i
}

func test() {
	// line := `callee[0]: ranges 734:18-67 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function GetWorkflowExecutionUpdateAcceptedEventAttributes in /Users/dan/src/temporalio/api-go/history/v1/message.pb.go:4856:24-73`
	line := `callee[9]: ranges 735:30-77, 742:30-77 in /Users/dan/src/temporalio/temporal/service/history/ndc/workflow_resetter.go from/to function AddWorkflowExecutionUpdateRequestReappliedEvent in /Users/dan/src/temporalio/temporal/service/history/workflow/mutable_state.go:162:3-50`
	result, err := parseCallSite(line)
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println(result)
	}
}

func testExampleResponse() {
	response, err := parseCallHierarchyResponse(exampleResponse)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s", err)
	}
	fmt.Print(response)
}

func Test() {
	test()
	testExampleResponse()
}
