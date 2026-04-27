package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Get or set baton configuration",
	}

	cmd.AddCommand(newConfigGetCmd(), newConfigSetCmd())
	return cmd
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "get <key>",
		Short:         "Get a configuration value",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			data, err := readConfigFile()
			if err != nil {
				return exitError(2, "reading config: %v", err)
			}

			val, err := getNestedValue(data, key)
			if err != nil {
				return exitError(1, "%v", err)
			}

			switch v := val.(type) {
			case string:
				fmt.Println(v)
			case []interface{}:
				for _, item := range v {
					fmt.Println(item)
				}
			default:
				out, _ := yaml.Marshal(val)
				fmt.Print(string(out))
			}
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "set <key> <value>",
		Short:         "Set a configuration value",
		Args:          cobra.ExactArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			data, err := readConfigFile()
			if err != nil {
				return exitError(2, "reading config: %v", err)
			}

			if err := setNestedValue(data, key, value); err != nil {
				return exitError(1, "%v", err)
			}

			if err := writeConfigFile(data); err != nil {
				return exitError(1, "writing config: %v", err)
			}

			fmt.Printf("%s = %s\n", key, value)
			return nil
		},
	}
}

func readConfigFile() (map[string]interface{}, error) {
	path := ".baton/agents.yaml"
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{}), nil
		}
		return nil, err
	}

	var data map[string]interface{}
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if data == nil {
		data = make(map[string]interface{})
	}
	return data, nil
}

func writeConfigFile(data map[string]interface{}) error {
	path := ".baton/agents.yaml"
	out, err := yaml.Marshal(data)
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func getNestedValue(data map[string]interface{}, key string) (interface{}, error) {
	parts := strings.Split(key, ".")
	var current interface{} = data

	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("key %q not found", key)
		}
		current, ok = m[part]
		if !ok {
			return nil, fmt.Errorf("key %q not found", key)
		}
	}
	return current, nil
}

func setNestedValue(data map[string]interface{}, key, value string) error {
	parts := strings.Split(key, ".")
	current := data

	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return nil
		}

		next, ok := current[part]
		if !ok {
			next = make(map[string]interface{})
			current[part] = next
		}

		nextMap, ok := next.(map[string]interface{})
		if !ok {
			nextMap = make(map[string]interface{})
			current[part] = nextMap
		}
		current = nextMap
	}
	return nil
}
