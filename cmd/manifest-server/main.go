// (c) Copyright 2017-2021 Matt Messier

package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/signal"
	"syscall"

	"github.com/jumptown-skydiving/manifest-server/pkg/core"
	"github.com/jumptown-skydiving/manifest-server/pkg/server"
	"github.com/jumptown-skydiving/manifest-server/pkg/settings"

	"golang.org/x/net/publicsuffix"
)

func newWebServer(app *core.Controller) (*server.WebServer, error) {
	settings := app.Settings()

	httpAddress := settings.WebServerAddress()
	httpsAddress := settings.WebServerSecureAddress()
	grpcAddress := settings.WebServerGRPCAddress()
	certFile := settings.ServerCertFile()
	keyFile := settings.ServerKeyFile()
	webServer, err := server.NewWebServer(app, httpAddress, httpsAddress,
		grpcAddress, certFile, keyFile)
	if err != nil {
		return nil, err
	}

	webServer.SetContentFunc("/settings.html", settings.HTML)
	webServer.SetContentFunc("/setconfig", settings.FormHandler)

	if jumprun := app.Jumprun(); jumprun != nil {
		webServer.SetContentFunc("/jumprun.html", jumprun.HTML)
		webServer.SetContentFunc("/setjumprun", jumprun.FormHandler)
	}

	webServer.SetContentFunc("/siwa", app.AppleEventHandler)

	return webServer, nil
}

func newSettings(configFilename string) (*settings.Settings, error) {
	if configFilename != "" {
		return settings.NewSettingsWithFilename(configFilename)
	}
	return settings.NewSettings()
}

func main() {
	var configFilename string
	flag.StringVar(&configFilename, "config", "", "specify config filename to use")
	flag.Parse()

	settings, err := newSettings(configFilename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	// Set up a cookie jar for the app to use. All HTTP requests will use
	// this cookie jar.
	jar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not create cookie jar: %v\n", err)
		os.Exit(1)
	}
	http.DefaultClient.Jar = jar

	app, err := core.NewController(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	settings.SetUpdateFunc(func(_ string) {
		app.WakeListeners(core.OptionsDataSource)
	})

	webServer, err := newWebServer(app)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot create web server: %v\n", err)
		os.Exit(1)
	}
	if err = webServer.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot start web server: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Server ready to service clients (pid %d)\n", os.Getpid())

	// Wait for shutdown signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c
	signal.Stop(c)

	fmt.Fprintf(os.Stderr, "Server stopping for receipt of termination signal\n")

	app.Close()
	webServer.Close()

	fmt.Fprintf(os.Stderr, "Server stopped\n")
}
