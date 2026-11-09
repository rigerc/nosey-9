package process

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestProcessHelper(_ *testing.T) {
	if os.Getenv("NOSEY_TEST_PROCESS") != "1" {
		return
	}
	switch os.Getenv("NOSEY_TEST_MODE") {
	case "echo":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(3)
		}
		encoded, err := json.Marshal(struct {
			Input string
			Args  []string
			Env   string
		}{string(data), os.Args, os.Getenv("NOSEY_TEST_VALUE")})
		if err != nil {
			os.Exit(3)
		}
		_, _ = os.Stdout.Write(encoded)
		_, _ = fmt.Fprint(os.Stderr, "separate diagnostic")
	case "large":
		_, _ = fmt.Fprint(os.Stdout, strings.Repeat("x", 2048))
	case "wait":
		time.Sleep(5 * time.Second)
	case "fail":
		os.Exit(7)
	}
	os.Exit(0)
}
func TestRunnerPreservesTokensInputAndEnvironment(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"NOSEY_TEST_PROCESS=1", "NOSEY_TEST_MODE=echo", "NOSEY_TEST_VALUE=present"}
	out, err := (Runner{}).RunInput(context.Background(), t.TempDir(), env, []byte(`{"request":1}`), executable, "-test.run=^TestProcessHelper$", "--", "$(not-a-command); *")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out.Stdout), "$(not-a-command); *") || !strings.Contains(string(out.Stdout), "present") || string(out.Stderr) != "separate diagnostic" {
		t.Fatal(out)
	}
	for _, mode := range []string{"large", "wait", "fail"} {
		out, err = (Runner{Timeout: 50 * time.Millisecond, Limit: 128}).Run(context.Background(), "", []string{"NOSEY_TEST_PROCESS=1", "NOSEY_TEST_MODE=" + mode}, executable, "-test.run=^TestProcessHelper$")
		if err == nil {
			t.Fatal("failure was treated as success", mode)
		}
		if len(out.Stdout) > 128 {
			t.Fatal("output budget exceeded")
		}
	}
}
