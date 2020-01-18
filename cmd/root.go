package cmd

import (
	"fmt"
	"github.com/4nte/go-mirror/internal"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"net/url"
	"os"
)

var ( // Flags
	cfgFile string
	Verbose bool

	sourceURI  string
	targetURI  string
	configFile string

	Topics []string
)

func isValidUrl(checkUrl string) error {
	parsed, err := url.Parse(checkUrl)
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

		fmt.Println("sooource", source)
		if err := isValidUrl(source); err != nil {
			return errors.Wrap(err, "failed to parse source URI")
		}
		if err := isValidUrl(target); err != nil {
			return errors.Wrap(err, "failed to target source URI")
		}

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
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

		topicFilter := viper.GetStringSlice("topic")
		isVerbose := viper.GetBool("verbose")

		sourceURL, err := url.Parse(source)
		if err != nil {
			panic(err)
		}
		targetURL, err := url.Parse(target)
		if err != nil {
			panic(err)
		}

		internal.Mirror(*sourceURL, *targetURL, topicFilter, isVerbose)
	},
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().StringSliceVarP(&Topics, "topic_filter", "t", []string{}, "comma separated topic filters. Example: foo,bar_*_health,device#")

	rootCmd.PersistentFlags().StringVar(&sourceURI, "source", "", "mqtt source URI")
	rootCmd.PersistentFlags().StringVar(&targetURI, "target", "", "mqtt target URI")

	rootCmd.PersistentFlags().StringVar(&targetURI, "config", "", "config file")

	err := viper.BindPFlag("source", rootCmd.PersistentFlags().Lookup("source"))
	if err != nil {
		panic(err)
	}

	viper.BindPFlag("target", rootCmd.PersistentFlags().Lookup("target"))
	viper.BindPFlag("topic_filter", rootCmd.PersistentFlags().Lookup("topic_filter"))
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
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
