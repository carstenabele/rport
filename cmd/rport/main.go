package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"

	chclient "github.com/cloudradar-monitoring/rport/client"
	chserver "github.com/cloudradar-monitoring/rport/server"
	chshare "github.com/cloudradar-monitoring/rport/share"
)

var help = `
  Usage: rport [command] [--help]

  Version: ` + chshare.BuildVersion + `

  Commands:
    server - runs rport in server mode
    client - runs rport in client mode

  Read more:
    https://github.com/cloudradar-monitoring/rport

`

func main() {

	version := flag.Bool("version", false, "")
	v := flag.Bool("v", false, "")
	flag.Bool("help", false, "")
	flag.Bool("h", false, "")
	flag.Usage = func() {}
	flag.Parse()

	if *version || *v {
		fmt.Println(chshare.BuildVersion)
		os.Exit(1)
	}

	args := flag.Args()

	subcmd := ""
	if len(args) > 0 {
		subcmd = args[0]
		args = args[1:]
	}

	switch subcmd {
	case "server":
		server(args)
	case "client":
		client(args)
	default:
		fmt.Fprintf(os.Stderr, help)
		os.Exit(1)
	}
}

var commonHelp = `
    --pid Generate pid file in current working directory

    -v, Enable verbose logging

    --help, This help text

  Signals:
    The rport process is listening for:
      a SIGUSR2 to print process stats, and
      a SIGHUP to short-circuit the client reconnect timer

  Version:
    ` + chshare.BuildVersion + ` (` + runtime.Version() + `)

  Read more:
    https://github.com/cloudradar-monitoring/rport

`

func generatePidFile() {
	pid := []byte(strconv.Itoa(os.Getpid()))
	if err := ioutil.WriteFile("rport.pid", pid, 0644); err != nil {
		log.Fatal(err)
	}
}

var serverHelp = `
  Usage: rport server [options]

  Options:

    --host, Defines the HTTP listening host – the network interface
    (defaults the environment variable HOST and falls back to 0.0.0.0).

    --port, -p, Defines the HTTP listening port (defaults to the environment
    variable PORT and fallsback to port 8080).

    --key, An optional string to seed the generation of a ECDSA public
    and private key pair. All communications will be secured using this
    key pair. Share the subsequent fingerprint with clients to enable detection
    of man-in-the-middle attacks (defaults to the RPORT_KEY environment
    variable, otherwise a new key is generate each run).

    --authfile, An optional path to a users.json file. This file should
    be an object with users defined like:
      {
        "<user:pass>": ["<addr-regex>","<addr-regex>"]
      }
    when <user> connects, their <pass> will be verified and then
    each of the remote addresses will be compared against the list
    of address regular expressions for a match.

    --auth, An optional string representing a single user with full
    access, in the form of <user:pass>. This is equivalent to creating an
    authfile with {"<user:pass>": [""]}.

    --proxy, Specifies another HTTP server to proxy requests to when
    rport receives a normal HTTP request. Useful for hiding rport in
    plain sight.

    --socks5, Allow clients to access the internal SOCKS5 proxy. See
    rport client --help for more information.

` + commonHelp

func server(args []string) {

	flags := flag.NewFlagSet("server", flag.ContinueOnError)

	host := flags.String("host", "", "")
	p := flags.String("p", "", "")
	port := flags.String("port", "", "")
	key := flags.String("key", "", "")
	authfile := flags.String("authfile", "", "")
	auth := flags.String("auth", "", "")
	proxy := flags.String("proxy", "", "")
	socks5 := flags.Bool("socks5", false, "")
	pid := flags.Bool("pid", false, "")
	verbose := flags.Bool("v", false, "")

	flags.Usage = func() {
		fmt.Print(serverHelp)
		os.Exit(1)
	}
	err := flags.Parse(args)
	if err != nil {
		fmt.Print(err)
		os.Exit(1)
	}

	if *host == "" {
		*host = os.Getenv("HOST")
	}
	if *host == "" {
		*host = "0.0.0.0"
	}
	if *port == "" {
		*port = *p
	}
	if *port == "" {
		*port = os.Getenv("PORT")
	}
	if *port == "" {
		*port = "8080"
	}
	if *key == "" {
		*key = os.Getenv("RPORT_KEY")
	}
	s, err := chserver.NewServer(&chserver.Config{
		KeySeed:  *key,
		AuthFile: *authfile,
		Auth:     *auth,
		Proxy:    *proxy,
		Socks5:   *socks5,
	})
	if err != nil {
		log.Fatal(err)
	}
	s.Debug = *verbose
	if *pid {
		generatePidFile()
	}
	go chshare.GoStats()
	if err = s.Run(*host, *port); err != nil {
		log.Fatal(err)
	}
}

type headerFlags struct {
	http.Header
}

func (flag *headerFlags) String() string {
	out := ""
	for k, v := range flag.Header {
		out += fmt.Sprintf("%s: %s\n", k, v)
	}
	return out
}

func (flag *headerFlags) Set(arg string) error {
	index := strings.Index(arg, ":")
	if index < 0 {
		return fmt.Errorf(`Invalid header (%s). Should be in the format "HeaderName: HeaderContent"`, arg)
	}
	if flag.Header == nil {
		flag.Header = http.Header{}
	}
	key := arg[0:index]
	value := arg[index+1:]
	flag.Header.Set(key, strings.TrimSpace(value))
	return nil
}

var clientHelp = `
  Usage: rport client [options] <server> <remote> [remote] [remote] ...

  <server> is the URL to the rport server.

  <remote>s are remote connections tunneled through the server, each of
  which come in the form:

    <local-interface>:<local-port>:<remote-host>:<remote-port>

  which does reverse port forwarding, sharing <remote-host>:<remote-port>
  from the client to the server's <local-interface>:<local-port>.

    example remotes

      3000
      example.com:3000
      3000:google.com:80
      192.168.0.5:3000:google.com:80
      socks
      5000:socks

    When the rport server has --socks5 enabled, remotes can
    specify "socks" in place of remote-host and remote-port.
    The default local host and port for a "socks" remote is
    127.0.0.1:1080. Connections to this remote will terminate
    at the server's internal SOCKS5 proxy.

    Remotes specifying "socks" will listen on the server's
    default socks port (1080) and terminate the connection at the
    client's internal SOCKS5 proxy.

  Options:

    --fingerprint, A *strongly recommended* fingerprint string
    to perform host-key validation against the server's public key.
    You may provide just a prefix of the key or the entire string.
    Fingerprint mismatches will close the connection.

    --auth, An optional username and password (client authentication)
    in the form: "<user>:<pass>". These credentials are compared to
    the credentials inside the server's --authfile. defaults to the
    AUTH environment variable.

    --keepalive, An optional keepalive interval. Since the underlying
    transport is HTTP, in many instances we'll be traversing through
    proxies, often these proxies will close idle connections. You must
    specify a time with a unit, for example '30s' or '2m'. Defaults
    to '0s' (disabled).

    --max-retry-count, Maximum number of times to retry before exiting.
    Defaults to unlimited.

    --max-retry-interval, Maximum wait time before retrying after a
    disconnection. Defaults to 5 minutes.

    --proxy, An optional HTTP CONNECT or SOCKS5 proxy which will be
    used to reach the rport server. Authentication can be specified
    inside the URL.
    For example, http://admin:password@my-server.com:8081
             or: socks://admin:password@my-server.com:1080

    --header, Set a custom header in the form "HeaderName: HeaderContent".
    Can be used multiple times. (e.g --header "Foo: Bar" --header "Hello: World")

    --hostname, Optionally set the 'Host' header (defaults to the host
    found in the server url).
` + commonHelp

func client(args []string) {
	flags := flag.NewFlagSet("client", flag.ContinueOnError)
	config := chclient.Config{Headers: http.Header{}}
	flags.StringVar(&config.Fingerprint, "fingerprint", "", "")
	flags.StringVar(&config.Auth, "auth", "", "")
	flags.DurationVar(&config.KeepAlive, "keepalive", 0, "")
	flags.IntVar(&config.MaxRetryCount, "max-retry-count", -1, "")
	flags.DurationVar(&config.MaxRetryInterval, "max-retry-interval", 0, "")
	flags.StringVar(&config.Proxy, "proxy", "", "")
	flags.Var(&headerFlags{config.Headers}, "header", "")
	hostname := flags.String("hostname", "", "")
	pid := flags.Bool("pid", false, "")
	verbose := flags.Bool("v", false, "")
	flags.Usage = func() {
		fmt.Print(clientHelp)
		os.Exit(1)
	}
	err := flags.Parse(args)
	if err != nil {
		fmt.Print(err)
		os.Exit(1)
	}
	//pull out options, put back remaining args
	args = flags.Args()
	if len(args) < 2 {
		log.Fatalf("A server and least one remote is required")
	}
	config.Server = args[0]
	config.Remotes = args[1:]
	//default auth
	if config.Auth == "" {
		config.Auth = os.Getenv("AUTH")
	}
	//move hostname onto headers
	if *hostname != "" {
		config.Headers.Set("Host", *hostname)
	}
	//ready
	c, err := chclient.NewClient(&config)
	if err != nil {
		log.Fatal(err)
	}
	c.Debug = *verbose
	if *pid {
		generatePidFile()
	}
	go chshare.GoStats()
	if err = c.Run(); err != nil {
		log.Fatal(err)
	}
}
