package playbook

import (
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// Condition evaluates conditional expressions for task execution
//
// Supported expressions:
//   - platform == "windows"
//   - platform != "linux"
//   - result.exit_code == 0
//   - result.stdout contains "installed"
//   - env.DEBUG == "true"
//   - variable_name == "value"
//   - true / false (literal)
//
// Operators: ==, !=, contains, not contains, and, or
type Condition struct {
	vars *Variables
}

// NewCondition creates a new condition evaluator
func NewCondition(vars *Variables) *Condition {
	return &Condition{vars: vars}
}

// Evaluate parses and evaluates a condition expression
func (c *Condition) Evaluate(expression string) (bool, error) {
	expression = strings.TrimSpace(expression)

	// Empty condition = always true
	if expression == "" {
		return true, nil
	}

	// Literal booleans
	if expression == "true" {
		return true, nil
	}
	if expression == "false" {
		return false, nil
	}

	// Handle 'and' operator (lowest precedence)
	if parts := splitOnOperator(expression, " and "); len(parts) > 1 {
		for _, part := range parts {
			result, err := c.Evaluate(part)
			if err != nil {
				return false, err
			}
			if !result {
				return false, nil // Short-circuit
			}
		}
		return true, nil
	}

	// Handle 'or' operator
	if parts := splitOnOperator(expression, " or "); len(parts) > 1 {
		for _, part := range parts {
			result, err := c.Evaluate(part)
			if err != nil {
				return false, err
			}
			if result {
				return true, nil // Short-circuit
			}
		}
		return false, nil
	}

	// Handle parentheses
	if strings.HasPrefix(expression, "(") && strings.HasSuffix(expression, ")") {
		return c.Evaluate(expression[1 : len(expression)-1])
	}

	// Handle 'not' operator
	if strings.HasPrefix(expression, "not ") {
		result, err := c.Evaluate(strings.TrimPrefix(expression, "not "))
		if err != nil {
			return false, err
		}
		return !result, nil
	}

	// Handle comparison operators
	return c.evaluateComparison(expression)
}

// evaluateComparison handles single comparison expressions
func (c *Condition) evaluateComparison(expression string) (bool, error) {
	// Check for "not contains" first (before "contains")
	if strings.Contains(expression, " not contains ") {
		parts := strings.SplitN(expression, " not contains ", 2)
		if len(parts) == 2 {
			left, err := c.resolveValue(strings.TrimSpace(parts[0]))
			if err != nil {
				return false, err
			}
			right, err := c.resolveValue(strings.TrimSpace(parts[1]))
			if err != nil {
				return false, err
			}
			return !strings.Contains(left, right), nil
		}
	}

	// Check for "contains"
	if strings.Contains(expression, " contains ") {
		parts := strings.SplitN(expression, " contains ", 2)
		if len(parts) == 2 {
			left, err := c.resolveValue(strings.TrimSpace(parts[0]))
			if err != nil {
				return false, err
			}
			right, err := c.resolveValue(strings.TrimSpace(parts[1]))
			if err != nil {
				return false, err
			}
			return strings.Contains(left, right), nil
		}
	}

	// Check for "!=" (before "==" to avoid partial match)
	if strings.Contains(expression, " != ") {
		parts := strings.SplitN(expression, " != ", 2)
		if len(parts) == 2 {
			left, err := c.resolveValue(strings.TrimSpace(parts[0]))
			if err != nil {
				return false, err
			}
			right, err := c.resolveValue(strings.TrimSpace(parts[1]))
			if err != nil {
				return false, err
			}
			return left != right, nil
		}
	}

	// Check for "=="
	if strings.Contains(expression, " == ") {
		parts := strings.SplitN(expression, " == ", 2)
		if len(parts) == 2 {
			left, err := c.resolveValue(strings.TrimSpace(parts[0]))
			if err != nil {
				return false, err
			}
			right, err := c.resolveValue(strings.TrimSpace(parts[1]))
			if err != nil {
				return false, err
			}
			return left == right, nil
		}
	}

	// Check for numeric comparisons: >, <, >=, <=
	for _, op := range []string{" >= ", " <= ", " > ", " < "} {
		if strings.Contains(expression, op) {
			parts := strings.SplitN(expression, op, 2)
			if len(parts) == 2 {
				leftStr, err := c.resolveValue(strings.TrimSpace(parts[0]))
				if err != nil {
					return false, err
				}
				rightStr, err := c.resolveValue(strings.TrimSpace(parts[1]))
				if err != nil {
					return false, err
				}

				left, errL := strconv.ParseFloat(leftStr, 64)
				right, errR := strconv.ParseFloat(rightStr, 64)
				if errL != nil || errR != nil {
					return false, fmt.Errorf("numeric comparison requires numeric values: %s", expression)
				}

				switch strings.TrimSpace(op) {
				case ">=":
					return left >= right, nil
				case "<=":
					return left <= right, nil
				case ">":
					return left > right, nil
				case "<":
					return left < right, nil
				}
			}
		}
	}

	// Single value - check if it's truthy
	val, err := c.resolveValue(expression)
	if err != nil {
		return false, err
	}
	return isTruthy(val), nil
}

// resolveValue resolves a value reference to its string value
func (c *Condition) resolveValue(ref string) (string, error) {
	ref = strings.TrimSpace(ref)

	// String literal (quoted)
	if (strings.HasPrefix(ref, "\"") && strings.HasSuffix(ref, "\"")) ||
		(strings.HasPrefix(ref, "'") && strings.HasSuffix(ref, "'")) {
		return ref[1 : len(ref)-1], nil
	}

	// Numeric literal
	if _, err := strconv.ParseFloat(ref, 64); err == nil {
		return ref, nil
	}

	// Built-in: platform
	if ref == "platform" {
		return runtime.GOOS, nil
	}

	// Built-in: arch
	if ref == "arch" {
		return runtime.GOARCH, nil
	}

	// Environment variable: env.VAR_NAME
	if strings.HasPrefix(ref, "env.") {
		envName := strings.TrimPrefix(ref, "env.")
		if val, ok := c.vars.Get("env." + envName); ok {
			return val, nil
		}
		// Try to get from environment directly
		val, _ := c.vars.Substitute("{{ env." + envName + " }}")
		if val != "{{ env."+envName+" }}" {
			return val, nil
		}
		return "", nil // Undefined env var = empty string
	}

	// Task result reference: result_name.property
	if strings.Contains(ref, ".") {
		parts := strings.SplitN(ref, ".", 2)
		taskName := parts[0]
		property := parts[1]

		if result, ok := c.vars.GetTaskResult(taskName); ok {
			switch property {
			case "stdout":
				return result.Stdout, nil
			case "stderr":
				return result.Stderr, nil
			case "exit_code":
				return strconv.Itoa(result.ExitCode), nil
			case "status":
				return string(result.Status), nil
			case "changed":
				return strconv.FormatBool(result.Changed), nil
			default:
				return "", fmt.Errorf("unknown task result property: %s", property)
			}
		}
	}

	// Regular variable lookup
	if val, ok := c.vars.Get(ref); ok {
		return val, nil
	}

	// Unknown reference - return as empty string (allows for undefined variable checks)
	return "", nil
}

// splitOnOperator splits an expression on an operator, respecting parentheses
func splitOnOperator(expr, op string) []string {
	var parts []string
	var current strings.Builder
	depth := 0

	// Simple split - doesn't handle nested parens perfectly but works for most cases
	tokens := strings.Split(expr, op)
	for i, token := range tokens {
		if i == 0 {
			current.WriteString(token)
		} else {
			// Check if previous part has unbalanced parens
			prev := current.String()
			openCount := strings.Count(prev, "(")
			closeCount := strings.Count(prev, ")")

			if openCount > closeCount {
				// We're inside parens, keep concatenating
				current.WriteString(op)
				current.WriteString(token)
				depth = openCount - closeCount
			} else {
				// Complete part
				parts = append(parts, strings.TrimSpace(current.String()))
				current.Reset()
				current.WriteString(token)
				depth = 0
			}
		}
	}

	// Add last part
	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}

	// If only one part after all processing, return nil to indicate no split
	if len(parts) <= 1 && depth == 0 {
		return nil
	}

	return parts
}

// isTruthy determines if a string value is considered "true"
func isTruthy(val string) bool {
	val = strings.TrimSpace(strings.ToLower(val))
	switch val {
	case "", "false", "0", "no", "off", "null", "nil", "none":
		return false
	default:
		return true
	}
}

// ValidateCondition checks if a condition expression is syntactically valid
// This is used during playbook parsing to catch errors early
func ValidateCondition(expression string) error {
	expression = strings.TrimSpace(expression)

	if expression == "" || expression == "true" || expression == "false" {
		return nil
	}

	// Check for balanced parentheses
	openCount := strings.Count(expression, "(")
	closeCount := strings.Count(expression, ")")
	if openCount != closeCount {
		return fmt.Errorf("unbalanced parentheses in condition: %s", expression)
	}

	// Check for valid operators
	validOperatorPattern := regexp.MustCompile(`(==|!=|>=|<=|>|<| contains | not contains | and | or |^not )`)
	if !validOperatorPattern.MatchString(expression) {
		// Could be a single variable reference - that's valid
		if !isValidIdentifier(expression) {
			return fmt.Errorf("invalid condition syntax: %s", expression)
		}
	}

	return nil
}

// isValidIdentifier checks if a string is a valid variable identifier
func isValidIdentifier(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}

	// Allow quoted strings
	if (strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
		(strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) {
		return true
	}

	// Allow numbers
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}

	// Check identifier pattern (allows dots for nested references)
	identPattern := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.]*$`)
	return identPattern.MatchString(s)
}
