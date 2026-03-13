package cmd

import (
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/4nte/mqtt-mirror/internal"
	"github.com/dchest/uniuri"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var ( // Flags
	cfgFile string
	Verbose bool

	sourceURI  string
	targetURI  string
	configFile string

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
	colonIdx := strings.Index(userinfo, ":")
	if colonIdx == -1 {
		username = userinfo
	} else {
		username = userinfo[:colonIdx]
		password = userinfo[colonIdx+1:]
	}

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
	Use:   "mqtt-mirror source target",
	Short: "lightweight traffic shadowing tool",
	Long:  `lightweight traffic shadowing tool purposely built to replicate traffic from one broker to another.`,
	Args: func(cmd *cobra.Command, args []string) error {
		var source, target string

		if len(args) == 2 {
			source = args[0]
			target = args[1]
			if len(args) < 2 {
				return errors.New("target and source not specified")
			}
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
			return errors.Wrap(err, "failed to parse source URI")
		}
		if err := isValidUrl(target); err != nil {
			return errors.Wrap(err, "failed to target source URI")
		}

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		sigs := make(chan os.Signal, 1)
		done := make(chan bool, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			sig := <-sigs
			fmt.Println()
			fmt.Println(sig)
			done <- true
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
			panic(err)
		}
		targetURL, err := parseBrokerURI(target)
		if err != nil {
			panic(err)
		}

		terminate, err := internal.Mirror(*sourceURL, *targetURL, topicFilter, isVerbose, 0, instanceName)
		if err != nil {
			panic(err)
		}

		<-done
		terminate()
	},
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().StringSliceVarP(&Topics, "topic_filter", "t", []string{}, "comma separated topic filters. Example: foo,bar_*_health,device#")

	rootCmd.PersistentFlags().StringVar(&sourceURI, "source", "", "mqtt source URI")
	rootCmd.PersistentFlags().StringVar(&targetURI, "target", "", "mqtt target URI")

	rootCmd.PersistentFlags().StringVarP(&instanceName, "name", "", "", "mqtt-mirror instance name. If not specified, will be randomly generated")

	rootCmd.PersistentFlags().StringVar(&targetURI, "config", "", "config file")

	err := viper.BindPFlag("source", rootCmd.PersistentFlags().Lookup("source"))
	if err != nil {
		panic(err)
	}

	viper.BindPFlag("target", rootCmd.PersistentFlags().Lookup("target"))
	viper.BindPFlag("topic_filter", rootCmd.PersistentFlags().Lookup("topic_filter"))
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	viper.BindPFlag("name", rootCmd.PersistentFlags().Lookup("name"))
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
