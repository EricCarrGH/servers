package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lrita/cmap"
	"github.com/madflojo/tasks"
)

/* https://github.com/avelino/awesome-go */
/* Confing ini files https://github.com/sasbury/mini */
/*
https://github.com/skx/evalfilter
https://github.com/madflojo/tasks
*/

var (
	WARN   CustomLogger
	INFO   CustomLogger
	ERROR  CustomLogger
	DEBUG  CustomLogger
	LOGGER CustomLogger
	DB     CustomLogger
)

type do_command func(*Client, string)

// This is our world!
var (
	COMMANDS  = make(map[string]do_command)
	CLIENTS   cmap.Map[string, *Client] // CLIENTS  cmap.Cmap
	SCHEDULER *tasks.Scheduler
	TIME      uint64
)

func main() {

	init_logger()
	init_os_signal()
	init_commands()
	init_scheduler()

	rand.Seed(time.Now().UnixNano())

	ServerAddr, _ := net.ResolveTCPAddr("tcp", ":1512")

	server, err := net.ListenTCP("tcp4", ServerAddr)
	if err != nil {
		ERROR.Fatalf("Unable to serve on localhost:1512 (%s)", err)
		return
	}
	defer server.Close()

	INFO.Println("Ready to serve on localhost:1512")

	for {
		conn, err := server.AcceptTCP()
		if err != nil {
			WARN.Printf("Unable to accept connection on localhost:1512 (%s)", err)
			return
		}
		go newClient(conn).clientLoop()
	}
}

/*
 *	Subsystems start here.
 */

func init_logger() {

	INFO = NewCustomLogger("info", "\u001b[32mINFO: \u001B[0m", log.LstdFlags)
	WARN = NewCustomLogger("warn", "\u001b[33mWARN: \u001B[0m", log.LstdFlags)
	ERROR = NewCustomLogger("error", "\u001b[31mERROR: \u001B[0m", log.LstdFlags)
	LOGGER = NewCustomLogger("logger", "\u001b[35mLOGGER: \u001B[0m", log.LstdFlags)
	DB = NewCustomLogger("db", "\u001b[31mDB: \u001B[0m", log.LstdFlags)
	DEBUG = NewCustomLogger("debug", "\u001b[36mDEBUG: \u001B[0m", log.LstdFlags|log.Lshortfile)

	value, ok := os.LookupEnv("LOG_LEVEL")

	if ok && value == "PROD" {
		DEBUG.SetActive(false)
	}
}

func update_log_level(logger string, onoff string) error {

	logger = strings.ToLower(logger)
	onoff = strings.ToLower(onoff)

	if logger == "logger" {
		LOGGER.Printf("unable to change the operation of the logger LOGGER")
		return fmt.Errorf("unable to change the operation of the logger '%s'", logger)
	}

	var newstatus bool

	switch onoff {
	case "on":
		newstatus = true
	case "off":
		newstatus = false
	default:
		LOGGER.Printf("unable to change logger to status '%s'. Only (on/off) are valid.", onoff)
		return fmt.Errorf("unable to change logger to status '%s'. Only (on/off) are valid", onoff)
	}

	switch logger {
	case "info":
		INFO.SetActive(newstatus)
	case "warn":
		WARN.SetActive(newstatus)
	case "error":
		ERROR.SetActive(newstatus)
	case "db":
		DB.SetActive(newstatus)
	case "debug":
		DEBUG.SetActive(newstatus)

	default:
		LOGGER.Printf("unable to update logger. '%s' is not a valid loglevel", logger)
		return fmt.Errorf("unable to update logger. '%s' is not a valid loglevel", logger)
	}

	LOGGER.Printf("logger '%s' updated to status '%s'", logger, onoff)

	return nil
}

func init_scheduler() error {
	SCHEDULER := tasks.New()

	TIME = 0

	SCHEDULER.Add(&tasks.Task{
		Interval: time.Duration(1 * time.Second),
		TaskFunc: ticker("a 1 sec ticker"),
	})

	return nil

}

// TODO, we should be able to add parameters to the function to exec w/o closures
func ticker(s string) func() error {

	return func() error {

		TIME += 1

		return nil
	}
}

func init_os_signal() {

	sigchnl := make(chan os.Signal, 1)
	signal.Notify(sigchnl)
	signal.Ignore(syscall.SIGURG, syscall.SIGWINCH) // SIGURG and SIGWINCH pop in macOS. Filter it out
	go SignalHandler(sigchnl)
}

func SignalHandler(sigchan chan os.Signal) {

	for {
		signal := <-sigchan

		switch signal {

		case syscall.SIGTERM:
			WARN.Println("Got SIGTERM. Program will terminate cleanly now.")
			Broadcast("Shutting down the server, it will re-start in a few minutes")
			os.Exit(0)
		case syscall.SIGINT:
			WARN.Println("Got SIGINT. Program will terminate cleanly now.")
			Broadcast("Shutting down the server, it will re-start in a few minutes")
			os.Exit(0)
		default:
			INFO.Printf("Received signal %s. No action taken.", signal)
		}
	}
}
