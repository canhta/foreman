package config

import (
	"fmt"
	"os"

	toml "github.com/pelletier/go-toml"
)

// AddAllowedNumber appends a phone number to channel.whatsapp.allowed_numbers in a TOML config file.
// Preserves comments and formatting using go-toml v1 tree manipulation.
func AddAllowedNumber(configPath string, phone string) error {
	tree, err := loadTree(configPath)
	if err != nil {
		return err
	}

	numbers := getAllowedNumbers(tree)
	for _, n := range numbers {
		if n == phone {
			return nil // already present
		}
	}
	numbers = append(numbers, phone)
	tree.SetPath([]string{"channel", "whatsapp", "allowed_numbers"}, numbers)

	return writeTree(configPath, tree)
}

// RemoveAllowedNumber removes a phone number from channel.whatsapp.allowed_numbers in a TOML config file.
func RemoveAllowedNumber(configPath string, phone string) error {
	tree, err := loadTree(configPath)
	if err != nil {
		return err
	}

	numbers := getAllowedNumbers(tree)
	filtered := make([]string, 0, len(numbers))
	for _, n := range numbers {
		if n != phone {
			filtered = append(filtered, n)
		}
	}
	tree.SetPath([]string{"channel", "whatsapp", "allowed_numbers"}, filtered)

	return writeTree(configPath, tree)
}

func loadTree(path string) (*toml.Tree, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	tree, err := toml.LoadBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return tree, nil
}

func writeTree(path string, tree *toml.Tree) error {
	out, err := tree.Marshal()
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func getAllowedNumbers(tree *toml.Tree) []string {
	val := tree.GetPath([]string{"channel", "whatsapp", "allowed_numbers"})
	if val == nil {
		return nil
	}
	arr, ok := val.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
