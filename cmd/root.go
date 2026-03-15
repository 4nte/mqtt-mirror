package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/4nte/mqtt-mirror/internal"
	"github.com/dchest/uniuri"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var version = "dev"

var ( // Flags
	cfgFile string
	Verbose bool

	sourceURI string
	targetURI string

	instanceName string

	Topics []string
)

// parseBrokerURI parses a raw broker URI, correctly handling special characters
// in the userinfo (username:password) portion that would otherwise confuse url.Parse.
func parseBrokerURI(rawURI string) (*url.URL, error) {
	schemeIdx := strings.Index(rawURI, "://")
	if schemeIdx == -1 {
		return url.Parse(rawURI)
	}

	scheme := rawURI[:schemeIdx]
	authority := rawURI[schemeIdx+3:] // everything after "://"

	// Find the last '@' — this separates userinfo from host, and we use the
	// last one because '@' may appear in the password.
	atIdx := strings.LastIndex(authority, "@")
	if atIdx == -1 {
		// No userinfo, parse directly
		return url.Parse(rawURI)
	}

	userinfo := authority[:atIdx]
	hostPart := authority[atIdx+1:]

	// Split userinfo on first ':' into username and password
	var username, password string
	username, password, _ = strings.Cut(userinfo, ":")

	// Unescape any existing percent-encoding so that url.UserPassword
	// can re-encode uniformly (avoids double-encoding).
	username, err := url.PathUnescape(username)
	if err != nil {
		return nil, fmt.Errorf("invalid username encoding: %w", err)
	}
	password, err = url.PathUnescape(password)
	if err != nil {
		return nil, fmt.Errorf("invalid password encoding: %w", err)
	}

	// Reconstruct with proper encoding via url.UserPassword
	encoded := url.UserPassword(username, password)
	reconstructed := fmt.Sprintf("%s://%s@%s", scheme, encoded.String(), hostPart)

	return url.Parse(reconstructed)
}

func isValidUrl(checkUrl string) error {
	parsed, err := parseBrokerURI(checkUrl)
	if err != nil {
		return err
	}

	if parsed.Host == "" && parsed.Port() == "" {
		return fmt.Errorf("invalid URI format. Example: tcp://localhost:1883 or tcp://foo:bar@localhost:1883")
	}

	return nil
}

var rootCmd = &cobra.Command{
	Use:     "mqtt-mirror source target",
	Version: version,
	Short:   "lightweight traffic shadowing tool",
	Long:    `lightweight traffic shadowing tool purposely built to replicate traffic from one broker to another.`,
	Args: func(cmd *cobra.Command, args []string) error {
		var source, target string

		if len(args) == 2 {
			source = args[0]
			target = args[1]
		} else {
			source = viper.GetString("source")
			target = viper.GetString("target")
		}

		if source == "" {
			return errors.New("mqtt source missing")
		}
		if target == "" {
			return errors.New("mqtt target missing")
		}

		if err := isValidUrl(source); err != nil {
			return fmt.Errorf("failed to parse source URI: %w", err)
		}
		if err := isValidUrl(target); err != nil {
			return fmt.Errorf("failed to parse target URI: %w", err)
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		sigs := make(chan os.Signal, 1)
		done := make(chan bool, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			sig := <-sigs
			fmt.Println()
			fmt.Println(sig)
			done <- true

			// Second signal forces immediate exit
			sig = <-sigs
			fmt.Println()
			fmt.Printf("%s (force quit)\n", sig)
			os.Exit(1)
		}()

		// Use args or viper value
		var source, target string
		if len(args) == 2 {
			source = args[0]
			target = args[1]
		}
		if source == "" {
			source = viper.GetString("source")
		}
		if target == "" {
			target = viper.GetString("target")
		}
		var instanceName = viper.GetString("name")
		if len(instanceName) == 0 {
			instanceName = uniuri.NewLen(8)

		}

		topicFilter := viper.GetStringSlice("topic_filter")
		isVerbose := viper.GetBool("verbose")

		sourceURL, err := parseBrokerURI(source)
		if err != nil {
			return fmt.Errorf("failed to parse source URI: %w", err)
		}
		targetURL, err := parseBrokerURI(target)
		if err != nil {
			return fmt.Errorf("failed to parse target URI: %w", err)
		}

		reg := prometheus.NewRegistry()
		reg.MustRegister(collectors.NewGoCollector())
		reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
		metrics := internal.NewMetrics(reg, version)

		healthPort := viper.GetInt("health_port")
		health := internal.NewHealthServer()
		if err := health.Start(healthPort, reg); err != nil {
			return fmt.Errorf("failed to start health server: %w", err)
		}

		cleanSession := viper.GetBool("clean_session")
		if !cleanSession && len(viper.GetString("name")) == 0 {
			fmt.Println("WARNING: --clean-session=false requires a stable client ID. Set --name to avoid session loss across restarts.")
		}

		// Build topic rewrite config
		rewriteConfig := internal.TopicRewriteConfig{
			Prefix: viper.GetString("topic_prefix"),
		}
		for _, raw := range viper.GetStringSlice("topic_replace") {
			r, err := internal.ParseTopicReplace(raw)
			if err != nil {
				return fmt.Errorf("invalid --topic-replace value: %w", err)
			}
			rewriteConfig.Replacements = append(rewriteConfig.Replacements, r)
		}

		publishTimeout := viper.GetDuration("publish_timeout")
		terminate, err := internal.Mirror(*sourceURL, *targetURL, topicFilter, isVerbose, 0, instanceName, cleanSession, health, metrics, rewriteConfig, publishTimeout)
		if err != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = health.Shutdown(ctx)
			return fmt.Errorf("mirror failed: %w", err)
		}

		<-done
		terminate()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = health.Shutdown(ctx)
		return nil
	},
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().StringSliceVarP(&Topics, "topic_filter", "t", []string{}, "comma separated topic filters. Example: foo,bar_*_health,device#")

	rootCmd.PersistentFlags().StringVar(&sourceURI, "source", "", "mqtt source URI")
	rootCmd.PersistentFlags().StringVar(&targetURI, "target", "", "mqtt target URI")

	rootCmd.PersistentFlags().StringVarP(&instanceName, "name", "", "", "mqtt-mirror instance name. If not specified, will be randomly generated")
	rootCmd.PersistentFlags().Int("health-port", 8080, "port for health check HTTP server")
	rootCmd.PersistentFlags().Bool("clean-session", true, "set MQTT clean session flag (use false for persistent sessions)")

	rootCmd.PersistentFlags().String("topic-prefix", "", "prefix to prepend to all mirrored topics")
	rootCmd.PersistentFlags().StringSlice("topic-replace", []string{}, "topic replacement in old:new format (repeatable)")
	rootCmd.PersistentFlags().Duration("publish-timeout", 10*time.Second, "timeout for publishing messages to the target broker")

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file")

	err := viper.BindPFlag("source", rootCmd.PersistentFlags().Lookup("source"))
	if err != nil {
		panic(err)
	}

	if err = viper.BindPFlag("target", rootCmd.PersistentFlags().Lookup("target")); err != nil {
		panic(err)
	}
	if err = viper.BindPFlag("topic_filter", rootCmd.PersistentFlags().Lookup("topic_filter")); err != nil {
		panic(err)
	}
	if err = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose")); err != nil {
		panic(err)
	}
	if err = viper.BindPFlag("name", rootCmd.PersistentFlags().Lookup("name")); err != nil {
		panic(err)
	}
	if err = viper.BindPFlag("health_port", rootCmd.PersistentFlags().Lookup("health-port")); err != nil {
		panic(err)
	}
	if err = viper.BindPFlag("clean_session", rootCmd.PersistentFlags().Lookup("clean-session")); err != nil {
		panic(err)
	}
	if err = viper.BindPFlag("topic_prefix", rootCmd.PersistentFlags().Lookup("topic-prefix")); err != nil {
		panic(err)
	}
	if err = viper.BindPFlag("topic_replace", rootCmd.PersistentFlags().Lookup("topic-replace")); err != nil {
		panic(err)
	}
	if err = viper.BindPFlag("publish_timeout", rootCmd.PersistentFlags().Lookup("publish-timeout")); err != nil {
		panic(err)
	}
}

func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath(".") // look for config in the working directory
		viper.SetConfigName("mirror")
		viper.SetConfigType("toml")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("using config file:", viper.ConfigFileUsed())
	}
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
