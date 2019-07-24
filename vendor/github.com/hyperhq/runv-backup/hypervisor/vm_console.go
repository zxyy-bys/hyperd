package hypervisor

import (
	"encoding/json"
	"fmt"
	"io"
	"net"

	"github.com/hyperhq/hypercontainer-utils/hlog"
	"github.com/hyperhq/runv/lib/telnet"
	"github.com/hyperhq/runv/lib/utils"
)

const (
	VmLogdSock = "/var/run/vmlogd.sock"
)

type LogMessage struct {
	Message string
	Id      string
	Path    string
}

func enableVmLogd(ctx *VmContext) error {
	conn, err := net.Dial("unix", VmLogdSock)
	if err != nil {
		return err
	}

	defer conn.Close()
	msg := LogMessage{
		Message: "start",
		Id:      ctx.Id,
		Path:    ctx.ConsoleSockName,
	}

	if err := json.NewEncoder(conn).Encode(&msg); err != nil {
		ctx.Log(ERROR, "fail to send message: %v", err)
		return err
	}

	if err := json.NewDecoder(conn).Decode(&msg); err != nil {
		ctx.Log(ERROR, "fail to receive message: %v", err)
		return err
	}

	if msg.Message != "success" || msg.Id != ctx.Id {
		return fmt.Errorf("fail to start vm logger")
	}

	return nil
}

func watchVmConsole(ctx *VmContext) {
	if err := enableVmLogd(ctx); err != nil {
		ctx.Log(TRACE, "fail to enable vmLogd: %v", err)
	} else {
		ctx.Log(TRACE, "log vm console through vmlogd")
		return
	}

	cout := make(chan string, 128)
	if dctx, ok := ctx.DCtx.(ConsoleDriverContext); ok {
		dctx.ConnectConsole(cout)
	} else {

		conn, err := utils.UnixSocketConnect(ctx.ConsoleSockName)
		if err != nil {
			ctx.Log(ERROR, "failed to connected to %s: %v", ctx.ConsoleSockName, err)
			return
		}

		ctx.Log(TRACE, "connected to %s", ctx.ConsoleSockName)

		tc, err := telnet.NewConn(conn)
		if err != nil {
			ctx.Log(ERROR, "fail to init telnet connection to %s: %v", ctx.ConsoleSockName, err)
			return
		}
		ctx.Log(TRACE, "connected %s as telnet mode.", ctx.ConsoleSockName)

		go TtyLiner(tc, cout)
	}
	const ignoreLines = 128
	for consoleLines := 0; consoleLines < ignoreLines; consoleLines++ {
		line, ok := <-cout
		if ok {
			ctx.Log(EXTRA, "[CNL] %s", line)
		} else {
			ctx.Log(TRACE, "console output end")
			return
		}
	}
	if !ctx.LogLevel(EXTRA) {
		ctx.Log(TRACE, "[CNL] omit the first %d line of console logs", ignoreLines)
	}
	for {
		line, ok := <-cout
		if ok {
			ctx.Log(TRACE, "[CNL] %s", line)
		} else {
			ctx.Log(TRACE, "console output end")
			return
		}
	}
}

func TtyLiner(conn io.Reader, output chan string) {
	buf := make([]byte, 1)
	line := []byte{}
	cr := false
	emit := false
	for {

		nr, err := conn.Read(buf)
		if err != nil || nr < 1 {
			hlog.Log(DEBUG, "Input byte chan closed, close the output string chan")
			close(output)
			return
		}
		switch buf[0] {
		case '\n':
			emit = !cr
			cr = false
		case '\r':
			emit = true
			cr = true
		default:
			cr = false
			line = append(line, buf[0])
		}
		if emit {
			output <- string(line)
			line = []byte{}
			emit = false
		}
	}
}
