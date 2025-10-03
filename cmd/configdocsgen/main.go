// Known Limitations/Further Improvements:
// Complex Slice/Map YAML Formatting: Default value formatting for slices is basic. Maps are not explicitly formatted for YAML beyond their default string representation.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/config" // Import your project's config package
)

var (
	// inputDir is no longer used for AST parsing but kept for potential future use or to avoid breaking existing scripts if any.
	inputDir   string
	outputFile string
)

// ReflectionFieldInfo is an intermediate struct to hold data extracted via reflection.
type ReflectionFieldInfo struct {
	FieldPath                string // Full dot-notation path from root config. e.g. "OTEL.Traces.Exporter"
	FieldName                string // Original Go field name. e.g. "Exporter"
	FieldTypeStr             string // String representation of field type. e.g. "string", "int", "config.OTELSignalExporterConfig"
	ParentGoStructName       string // Go type name of the struct this field directly belongs to. e.g. "OTELSignalConfig"
	ParentGoStructPkgPath    string // Package path of the Go struct this field directly belongs to.
	ParentStructInstancePath string // Unique path to the instance of the parent struct. e.g. "Config.OTEL.Traces"
	YAMLName                 string
	EnvName                  string
	EnvSeparator             string
	Description              string
	Required                 string      // "true", "false", or "Y", "N", "C" from tag
	DefaultValue             interface{} // Actual default value
	IsEmbeddedField          bool        // True if this field comes from an embedded struct and should be inlined in YAML
}

// ConfigField represents a field in a configuration struct (used by generateDocs)
type ConfigField struct {
	Name         string
	Type         string
	YAMLName     string
	EnvName      string
	EnvSeparator string
	Description  string
	Required     string // Y, N, C
	DefaultValue string // String representation
}

// ParsedConfig represents a parsed configuration struct (used by generateDocs)
type ParsedConfig struct {
	FileName     string // Can be set to "reflection" or similar
	Name         string // Go struct name (e.g., "Config", "RedisConfig")
	Fields       []ConfigField
	IsTopLevel   bool   // Flag to identify the root config.Config struct for pathing
	GoStructName string // Same as Name
}

func main() {
	defaultInputDir := "internal/config" // Retained for consistency, though not directly used for parsing config.Config
	defaultOutputFile := "docs/pages/references/configuration.mdx"

	flag.StringVar(&inputDir, "input-dir", defaultInputDir, "Directory containing the Go configuration source files (usage changed with reflection).")
	flag.StringVar(&outputFile, "output-file", defaultOutputFile, "Path to the output Markdown file.")
	flag.Parse()

	fmt.Println("Configuration Documentation Generator (Reflection-based)")
	log.Printf("Output file: %s", outputFile)

	allFieldInfos, err := parseConfigWithReflection()
	if err != nil {
		log.Fatalf("Error parsing config with reflection: %v", err)
	}

	parsedConfigs := transformToParsedConfigs(allFieldInfos)

	err = generateDocs(parsedConfigs, outputFile)
	if err != nil {
		log.Fatalf("Error generating docs: %v", err)
	}

	fmt.Printf("Successfully generated documentation to %s\n", outputFile)
}

func parseConfigWithReflection() ([]ReflectionFieldInfo, error) {
	var infos []ReflectionFieldInfo
	cfg := config.Config{}
	cfg.InitDefaults()

	cfgType := reflect.TypeOf(cfg)
	cfgValue := reflect.ValueOf(cfg)
	targetPkgPath := cfgType.PkgPath()

	err := extractFieldsRecursive(cfgValue, cfgType, cfgType.Name(), cfgType.Name(), cfgType.PkgPath(), targetPkgPath, &infos, false) // Initial pathPrefix is cfgType.Name()
	if err != nil {
		return nil, err
	}
	return infos, nil
}

func extractFieldsRecursive(currentVal reflect.Value, currentType reflect.Type, parentStructInstancePath string, parentGoStructName string, parentGoStructPkgPath string, targetPkgPath string, infos *[]ReflectionFieldInfo, isEmbedded bool) error {
	if currentType.Kind() == reflect.Ptr {
		if currentVal.IsNil() {
			currentType = currentType.Elem()
			currentVal = reflect.Zero(currentType)
		} else {
			currentVal = currentVal.Elem()
			currentType = currentType.Elem()
		}
	}

	if currentType.Kind() != reflect.Struct {
		return fmt.Errorf("expected a struct or pointer to a struct, got %s", currentType.Kind())
	}

	for i := 0; i < currentType.NumField(); i++ {
		fieldSpec := currentType.Field(i)
		fieldVal := currentVal.Field(i)

		if !fieldVal.CanInterface() {
			continue
		}

		fieldName := fieldSpec.Name
		fieldPath := fieldName
		if parentStructInstancePath != "" {
			fieldPath = parentStructInstancePath + "." + fieldName
		} else {
			// This case should ideally only happen for the root struct's direct fields if parentStructInstancePath for root is empty.
			// However, we initialize parentStructInstancePath to cfgType.Name() for the root.
		}

		if fieldSpec.Anonymous {
			// For embedded structs, recurse with the same parentStructInstancePath
			// The parentGoStructName and parentGoStructPkgPath also remain the same for the fields of the embedded struct.
			err := extractFieldsRecursive(fieldVal, fieldSpec.Type, parentStructInstancePath, parentGoStructName, parentGoStructPkgPath, targetPkgPath, infos, true)
			if err != nil {
				return fmt.Errorf("error recursing into embedded struct %s: %w", fieldName, err)
			}
			continue
		}

		yamlTag := fieldSpec.Tag.Get("yaml")
		yamlName := strings.Split(yamlTag, ",")[0]
		if yamlName == "-" {
			continue
		}
		if yamlName == "" {
			yamlName = fieldName
		}

		fieldTypeStr := formatFieldType(fieldSpec.Type)

		currentFieldInfo := ReflectionFieldInfo{
			FieldPath:                fieldPath,
			FieldName:                fieldName,
			FieldTypeStr:             fieldTypeStr,
			ParentGoStructName:       parentGoStructName, // Go type name of the struct this field *directly* belongs to
			ParentGoStructPkgPath:    parentGoStructPkgPath,
			ParentStructInstancePath: parentStructInstancePath, // Path to the instance of the parent struct
			YAMLName:                 yamlName,
			EnvName:                  fieldSpec.Tag.Get("env"),
			EnvSeparator:             fieldSpec.Tag.Get("envSeparator"),
			Description:              fieldSpec.Tag.Get("desc"),
			Required:                 fieldSpec.Tag.Get("required"),
			DefaultValue:             fieldVal.Interface(),
			IsEmbeddedField:          isEmbedded,
		}
		*infos = append(*infos, currentFieldInfo)

		actualFieldType := fieldSpec.Type
		if actualFieldType.Kind() == reflect.Ptr {
			actualFieldType = actualFieldType.Elem()
		}

		// Recurse if it's a struct from the target package (and not time.Time)
		if fieldSpec.Type.Kind() != reflect.Interface && actualFieldType.Kind() == reflect.Struct && actualFieldType.PkgPath() == targetPkgPath && actualFieldType.Name() != "Time" {
			// The new parentStructInstancePath for the recursive call is the FieldPath of the current struct field.
			// The new parentGoStructName for the recursive call is the Go type name of this nested struct field.
			err := extractFieldsRecursive(fieldVal, fieldSpec.Type, fieldPath, actualFieldType.Name(), actualFieldType.PkgPath(), targetPkgPath, infos, false)
			if err != nil {
				return fmt.Errorf("error recursing into members of struct field %s (type %s): %w", fieldPath, actualFieldType.Name(), err)
			}
		}
	}
	return nil
}

func formatFieldType(t reflect.Type) string {
	// This produces types like "string", "int", "[]string", "map[string]int", "config.RedisConfig", "*config.MQsConfig"
	// which is generally good for documentation.
	return t.String()
}

func isTargetPkgStruct(t reflect.Type, targetPkgPath string) bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct && t.PkgPath() == targetPkgPath && t.Name() != "Time" // Exclude time.Time
}

func formatDefaultValueToString(value interface{}, goType string) string {
	if value == nil {
		return "" // generateDocs will convert this to `nil`
	}

	val := reflect.ValueOf(value)
	if val.Kind() == reflect.Ptr && val.IsNil() {
		return ""
	}

	// Handle time.Time specifically if needed, otherwise %v is usually okay.
	if t, ok := value.(time.Time); ok {
		if t.IsZero() {
			return "" // Represent zero time as empty for cleaner defaults
		}
		return t.Format(time.RFC3339) // Or another suitable format
	}

	// Handle slices
	if val.Kind() == reflect.Slice {
		if val.IsNil() { // Explicitly nil slice
			return ""
		}
		if val.Len() == 0 { // Empty slice []
			return "[]"
		}
		// For non-empty slices, fmt.Sprintf("%v", value) gives "[elem1 elem2 ...]"
		// We might want commas: "[elem1, elem2]"
		var parts []string
		for i := 0; i < val.Len(); i++ {
			parts = append(parts, fmt.Sprintf("%v", val.Index(i).Interface()))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}

	// Handle booleans to ensure "true" or "false"
	if val.Kind() == reflect.Bool {
		return strconv.FormatBool(val.Bool())
	}

	// Default string representation
	strVal := fmt.Sprintf("%v", value)

	// Avoid Go's default "<nil>" string for non-interface nils (e.g. nil map/slice that wasn't caught above)
	if strVal == "<nil>" {
		return ""
	}

	return strVal
}

func transformToParsedConfigs(infos []ReflectionFieldInfo) []ParsedConfig {
	groupedByInstancePath := make(map[string][]ReflectionFieldInfo)
	structInstancePathOrder := []string{} // To maintain an order

	rootConfigInstancePath := "" // Will be determined by the first info, assuming root is processed first or by checking PkgPath

	for _, info := range infos {
		// Attempt to identify the root config instance path (e.g., "Config")
		if rootConfigInstancePath == "" && info.ParentGoStructName == "Config" && info.ParentGoStructPkgPath == "github.com/hookdeck/outpost/internal/config" {
			// The ParentStructInstancePath for fields directly under Config will be "Config"
			// This logic assumes the initial call to extractFieldsRecursive for config.Config uses "Config" as pathPrefix.
			if strings.Count(info.ParentStructInstancePath, ".") == 0 { // e.g. "Config", not "Config.Foo"
				rootConfigInstancePath = info.ParentStructInstancePath
			}
		}

		if _, exists := groupedByInstancePath[info.ParentStructInstancePath]; !exists {
			groupedByInstancePath[info.ParentStructInstancePath] = []ReflectionFieldInfo{}
			structInstancePathOrder = append(structInstancePathOrder, info.ParentStructInstancePath)
		}
		groupedByInstancePath[info.ParentStructInstancePath] = append(groupedByInstancePath[info.ParentStructInstancePath], info)
	}
	if rootConfigInstancePath == "" && len(structInstancePathOrder) > 0 {
		// Fallback: assume the shortest path is the root, or the one named "Config" if available
		for _, p := range structInstancePathOrder {
			if p == "Config" { // Default root struct name
				rootConfigInstancePath = p
				break
			}
		}
		if rootConfigInstancePath == "" {
			// As a last resort, pick the first one if only one, or sort and pick shortest.
			// This part might need refinement if the root config isn't named "Config".
			sort.Strings(structInstancePathOrder) // Sort alphabetically to have a deterministic order
			if len(structInstancePathOrder) > 0 {
				rootConfigInstancePath = structInstancePathOrder[0] // Default to first after sort
				log.Printf("Warning: Could not definitively determine root config instance path. Defaulting to '%s'. Ensure root config is named 'Config' or initial pathPrefix is set correctly.", rootConfigInstancePath)
			}
		}
	}

	// Sort structInstancePathOrder: root path first, then alphabetically.
	sort.SliceStable(structInstancePathOrder, func(i, j int) bool {
		pathI := structInstancePathOrder[i]
		pathJ := structInstancePathOrder[j]
		if pathI == rootConfigInstancePath {
			return true
		}
		if pathJ == rootConfigInstancePath {
			return false
		}
		// Sort by depth first (fewer dots), then alphabetically
		depthI := strings.Count(pathI, ".")
		depthJ := strings.Count(pathJ, ".")
		if depthI != depthJ {
			return depthI < depthJ
		}
		return pathI < pathJ
	})

	var parsedConfigs []ParsedConfig
	for _, instancePath := range structInstancePathOrder {
		fieldsInfo := groupedByInstancePath[instancePath]
		if len(fieldsInfo) == 0 { // Should not happen if instancePath came from the map keys
			continue
		}
		var configFields []ConfigField
		// All fields in fieldsInfo share the same ParentGoStructName and ParentStructInstancePath.
		// The ParentGoStructName is the Go type of the struct instance represented by 'instancePath'.
		parentGoStructName := fieldsInfo[0].ParentGoStructName // Safe due to check above

		// Sort fields by their original Go field name for consistent order within this instance
		sort.Slice(fieldsInfo, func(i, j int) bool {
			return fieldsInfo[i].FieldName < fieldsInfo[j].FieldName
		})

		for _, info := range fieldsInfo {
			// We only add fields that directly belong to this ParentStructInstancePath.
			// Embedded fields are handled by IsEmbeddedField flag if needed later, but here we list them.
			configFields = append(configFields, ConfigField{
				Name:         info.FieldName,
				Type:         info.FieldTypeStr,
				YAMLName:     info.YAMLName,
				EnvName:      info.EnvName,
				EnvSeparator: info.EnvSeparator,
				Description:  info.Description,
				Required:     info.Required,
				DefaultValue: formatDefaultValueToString(info.DefaultValue, info.FieldTypeStr),
			})
		}

		isTopLevel := (instancePath == rootConfigInstancePath)

		parsedConfigs = append(parsedConfigs, ParsedConfig{
			FileName:     "reflection-generated",
			Name:         instancePath,       // Unique instance path, e.g., "Config.OTEL.Traces"
			GoStructName: parentGoStructName, // Go type name, e.g., "OTELSignalConfig"
			Fields:       configFields,
			IsTopLevel:   isTopLevel,
		})
	}
	return parsedConfigs
}

const (
	envVarsStartPlaceholder = "{/* BEGIN AUTOGENERATED CONFIG ENV VARS */}"
	envVarsEndPlaceholder   = "{/* END AUTOGENERATED CONFIG ENV VARS */}"
	yamlStartPlaceholder    = "{/* BEGIN AUTOGENERATED CONFIG YAML */}"
	yamlEndPlaceholder      = "{/* END AUTOGENERATED CONFIG YAML */}"
)

func generateDocs(parsedConfigs []ParsedConfig, outputPath string) error {
	// --- Generate ENV VARS Table Content (including headers) ---
	var envVarsBuilder strings.Builder
	envVarsBuilder.WriteString("| Variable | Description | Default | Required |\n")
	envVarsBuilder.WriteString("|----------|-------------|---------|----------|\n")
	envVarFields := collectEnvVarFields(parsedConfigs)
	for _, field := range envVarFields {
		if field.EnvName == "" {
			continue
		}
		requiredText := formatRequiredText(field.Required)
		defaultValueText := field.DefaultValue
		if defaultValueText == "" || defaultValueText == "<nil>" { // Handle <nil> from Sprintf as well
			defaultValueText = "`nil`"
		} else {
			// Escape special characters for Markdown table cells
			defaultValueText = strings.ReplaceAll(defaultValueText, "|", "\\|")
			// Enclose in backticks if not already `nil`
			defaultValueText = fmt.Sprintf("`%s`", defaultValueText)
		}

		descriptionText := strings.ReplaceAll(field.Description, "|", "\\|")
		descriptionText = strings.ReplaceAll(descriptionText, "\n", " ") // Ensure description is single line for table
		// Escape curly braces for MDX in descriptions
		descriptionText = strings.ReplaceAll(descriptionText, "{", "\\{")
		descriptionText = strings.ReplaceAll(descriptionText, "}", "\\}")

		envVarsBuilder.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n",
			field.EnvName,
			descriptionText,
			defaultValueText,
			requiredText,
		))
	}
	envVarsContent := strings.TrimRight(envVarsBuilder.String(), "\n")

	// --- Generate YAML Content (including fences) ---
	var yamlBuilder strings.Builder
	yamlBuilder.WriteString("```yaml\n")
	yamlBuilder.WriteString("# Outpost Configuration Example (Generated)\n")
	yamlBuilder.WriteString("# This example shows all available keys with their default values where applicable.\n\n")

	var mainConfigInfo *ParsedConfig
	configInfoMap := make(map[string]*ParsedConfig) // Keyed by ParsedConfig.Name (ParentStructInstancePath)
	rootConfigInstancePath := ""

	for i := range parsedConfigs {
		pc := &parsedConfigs[i]
		configInfoMap[pc.Name] = pc // pc.Name is now the ParentStructInstancePath
		if pc.IsTopLevel {
			mainConfigInfo = pc
			rootConfigInstancePath = pc.Name // Store the root instance path
		}
	}

	if mainConfigInfo != nil {
		generateYAMLPart(&yamlBuilder, mainConfigInfo, configInfoMap, 0, true)
	} else {
		foundRoot := false
		// Attempt to find the root config using rootConfigInstancePath if IsTopLevel set it
		if rootConfigInstancePath != "" {
			if cfgByRootPath, ok := configInfoMap[rootConfigInstancePath]; ok {
				log.Printf("Info: Main config not directly found by IsTopLevel flag, but using identified root instance path '%s'.", rootConfigInstancePath)
				generateYAMLPart(&yamlBuilder, cfgByRootPath, configInfoMap, 0, true)
				foundRoot = true
			} else {
				log.Printf("Warning: rootConfigInstancePath '%s' was set (likely by IsTopLevel processing) but its corresponding entry was not found in configInfoMap. Proceeding with other fallbacks.", rootConfigInstancePath)
			}
		}

		// If not found via rootConfigInstancePath, try falling back to "Config" by name
		if !foundRoot {
			if cfgByName, ok := configInfoMap["Config"]; ok {
				log.Println("Warning: Main config not found by IsTopLevel flag or specific root path. Falling back to instance path 'Config'.")
				generateYAMLPart(&yamlBuilder, cfgByName, configInfoMap, 0, true)
				foundRoot = true
			}
		}

		// If still not found, and parsedConfigs exist, try the shortest path as a heuristic
		if !foundRoot && len(parsedConfigs) > 0 {
			// Create a copy for sorting, as parsedConfigs itself might be used elsewhere or iterating over it.
			sortedConfigs := make([]ParsedConfig, len(parsedConfigs))
			copy(sortedConfigs, parsedConfigs)
			sort.SliceStable(sortedConfigs, func(i, j int) bool {
				lenI := len(sortedConfigs[i].Name)
				lenJ := len(sortedConfigs[j].Name)
				if lenI != lenJ {
					return lenI < lenJ // Shorter paths first
				}
				return sortedConfigs[i].Name < sortedConfigs[j].Name // Then alphabetically
			})
			potentialRoot := &sortedConfigs[0]
			log.Printf("Warning: Main config not found by IsTopLevel, specific root path, or 'Config' name. Falling back to first parsed config by path length: '%s'.", potentialRoot.Name)
			generateYAMLPart(&yamlBuilder, potentialRoot, configInfoMap, 0, true)
			foundRoot = true
		}

		// If no root could be determined by any means
		if !foundRoot {
			yamlBuilder.WriteString("# ERROR: Main configuration struct not found. Cannot generate YAML structure.\n")
			log.Println("Error: Main configuration struct not found. Cannot determine entry point for YAML generation.")
		}
	}
	yamlBuilder.WriteString("```\n")
	yamlContent := yamlBuilder.String()

	// --- Read existing MDX file ---
	mdxBytes, err := os.ReadFile(outputPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Warning: Output file %s does not exist. A new file will be created with placeholders.", outputPath)
			// Create a template with placeholders if file doesn't exist
			templateContent := fmt.Sprintf(`---
title: Configuration Reference
description: Detailed configuration options for Outpost.
---

This document outlines all the configuration options available for Outpost, settable via environment variables or a YAML configuration file.

## Environment Variables

%s

%s

%s

## YAML Configuration

Below is an example YAML configuration file showing all available options and their default values.

%s

%s

%s
`, envVarsStartPlaceholder, envVarsContent, envVarsEndPlaceholder, yamlStartPlaceholder, yamlContent, yamlEndPlaceholder)
			mdxBytes = []byte(templateContent)
		} else {
			return fmt.Errorf("failed to read output file %s: %w", outputPath, err)
		}
	}
	mdxContent := string(mdxBytes)

	// --- Replace placeholders ---
	envStartIndex := strings.Index(mdxContent, envVarsStartPlaceholder)
	envEndIndex := strings.Index(mdxContent, envVarsEndPlaceholder)

	if envStartIndex != -1 && envEndIndex != -1 && envEndIndex > envStartIndex {
		newMdxContent, changed := replacePlaceholder(mdxContent, envVarsStartPlaceholder, envVarsEndPlaceholder, "\n"+envVarsContent+"\n")
		if !changed {
			// Placeholder found, but content was already up-to-date.
			log.Printf("Info: The content for the ENV vars placeholder '%s' was already up-to-date. No changes made to this block.", envVarsStartPlaceholder)
		} else {
			mdxContent = newMdxContent // Content was updated.
		}
	} else {
		// Placeholder not found or in wrong order.
		log.Printf("Warning: The ENV vars placeholder '%s' (and/or its corresponding end tag '%s') was not found or is in the wrong order in the output file. The ENV vars block will not be updated.", envVarsStartPlaceholder, envVarsEndPlaceholder)
	}

	yamlStartIndex := strings.Index(mdxContent, yamlStartPlaceholder)
	yamlEndIndex := strings.Index(mdxContent, yamlEndPlaceholder)

	if yamlStartIndex != -1 && yamlEndIndex != -1 && yamlEndIndex > yamlStartIndex {
		newMdxContent, changed := replacePlaceholder(mdxContent, yamlStartPlaceholder, yamlEndPlaceholder, "\n"+yamlContent+"\n")
		if !changed {
			// Placeholder found, but content was already up-to-date.
			log.Printf("Info: The content for the YAML placeholder '%s' was already up-to-date. No changes made to this block.", yamlStartPlaceholder)
		} else {
			mdxContent = newMdxContent // Content was updated.
		}
	} else {
		// Placeholder not found or in wrong order.
		log.Printf("Warning: The YAML placeholder '%s' (and/or its corresponding end tag '%s') was not found or is in the wrong order in the output file. The YAML block will not be updated.", yamlStartPlaceholder, yamlEndPlaceholder)
	}

	// --- Write updated content back to MDX file ---
	err = os.WriteFile(outputPath, []byte(mdxContent), 0644)
	if err != nil {
		return fmt.Errorf("failed to write updated content to %s: %w", outputPath, err)
	}

	return nil
}

func replacePlaceholder(content, startPlaceholder, endPlaceholder, newBlockContent string) (string, bool) {
	startIndex := strings.Index(content, startPlaceholder)
	endIndex := strings.Index(content, endPlaceholder)

	if startIndex != -1 && endIndex != -1 && endIndex > startIndex {
		// Include the start placeholder, replace content until end placeholder, then add end placeholder
		newContent := content[:startIndex+len(startPlaceholder)] +
			newBlockContent +
			content[endIndex:]
		return newContent, newContent != content
	}
	return content, false // Placeholder not found or content is the same
}

func generateYAMLPart(builder *strings.Builder, configInfo *ParsedConfig, allConfigs map[string]*ParsedConfig, indentLevel int, isRoot bool) {
	indent := strings.Repeat("  ", indentLevel)

	// Sort fields by YAMLName for consistent YAML output
	// This is important because map iteration order is not guaranteed in Go for allConfigs
	// and field order from reflection is Go struct order, not necessarily desired YAML order.
	// However, for struct fields, we use the order from ParsedConfig.Fields which is
	// now sorted by Go FieldName in transformToParsedConfigs.
	// For truly aesthetic YAML, one might want a custom sort order.
	// For now, using the order from ParsedConfig.Fields.

	for _, field := range configInfo.Fields {
		if field.YAMLName == "" || field.YAMLName == "-" { // Skip if no YAML name or explicitly ignored
			continue
		}
		// Field description as a comment
		if field.Description != "" {
			// Ensure multi-line descriptions are commented correctly
			descLines := strings.Split(field.Description, "\n")
			for _, line := range descLines {
				builder.WriteString(fmt.Sprintf("%s# %s\n", indent, strings.TrimSpace(line)))
			}
		}

		// Removed default value comments as per feedback. The value itself is shown.
		// if field.DefaultValue != "" && !isStructTypeField {
		// 	builder.WriteString(fmt.Sprintf("%s# Default: %s\n", indent, field.DefaultValue))
		// } else if field.DefaultValue == "" && !isStructTypeField && field.Required != "Y" && field.Required != "true" {
		// 	// Indicate if no default and not required (optional)
		// 	builder.WriteString(fmt.Sprintf("%s# Default: (none)\n", indent))
		// }

		// Required status as a comment
		if field.Required != "" && field.Required != "N" && field.Required != "false" {
			requiredText := field.Required
			if field.Required == "C" {
				requiredText = "Conditional"
			}
			builder.WriteString(fmt.Sprintf("%s# Required: %s\n", indent, requiredText))
		}

		// Field line
		builder.WriteString(fmt.Sprintf("%s%s:", indent, field.YAMLName))

		// Construct the potential instance path for the nested struct.
		// configInfo.Name is the instance path of the current struct (e.g., "Config.OTEL").
		// field.Name is the Go field name of the current field (e.g., "Traces").
		nestedStructInstancePath := configInfo.Name + "." + field.Name

		// Check if this instance path exists in our map of parsed configs.
		// allConfigs is keyed by ParentStructInstancePath (which is ParsedConfig.Name).
		if nestedConfig, ok := allConfigs[nestedStructInstancePath]; ok && nestedConfig.GoStructName != "Time" {
			// It's a nested struct instance we should expand, and it's not time.Time.
			// The ParsedConfig for this instance (nestedConfig) contains its fields.
			builder.WriteString("\n")
			generateYAMLPart(builder, nestedConfig, allConfigs, indentLevel+1, false)
		} else {
			// It's a scalar, slice, map, time.Time, or a struct from a different package/type not further detailed by a ParsedConfig entry.
			valueStr := field.DefaultValue
			if valueStr == "" || (valueStr == "[]" && strings.HasPrefix(field.Type, "[]")) {
				// If default is empty or an empty slice representation for a slice type, use placeholder.
				valueStr = getYAMLPlaceholderForType(field.Type)
			} else {
				valueStr = formatSimpleValueToYAML(valueStr, field.Type)
			}
			builder.WriteString(fmt.Sprintf(" %s\n", valueStr))
		}
		builder.WriteString("\n") // Add a blank line after each top-level entry in a struct for readability
	}
}

func getYAMLPlaceholderForType(goType string) string {
	switch {
	case strings.HasPrefix(goType, "[]string"):
		return "[item1, item2]"
	case strings.HasPrefix(goType, "[]"): // Generic slice
		return "[]"
	case strings.HasPrefix(goType, "map["):
		return "{key: value}"
	case goType == "string":
		return "\"\""
	case goType == "int", goType == "int64", goType == "float64":
		return "0"
	case goType == "bool":
		return "false"
	default:
		return "# <" + goType + ">"
	}
}

func formatSimpleValueToYAML(value, goType string) string {
	// If it's a string, ensure it's quoted if it contains special chars or is empty
	if goType == "string" {
		if value == "" {
			return "\"\"" // Explicitly empty string
		}
		// Always quote non-empty strings to handle all special characters correctly for YAML.
		return strconv.Quote(value)
	}
	if value == "[]" && strings.HasPrefix(goType, "[]") { // Empty slice from default
		return "[]"
	}
	// For non-string types (numbers, booleans), the default fmt.Sprintf("%v") representation is usually fine for YAML.
	return value
}

// collectEnvVarFields gathers all fields that have an EnvName, from all ParsedConfig structs.
func collectEnvVarFields(parsedConfigs []ParsedConfig) []ConfigField {
	var allFields []ConfigField
	seenEnvVars := make(map[string]bool) // To avoid duplicates if structs are processed multiple times or nested weirdly

	// Process top-level "Config" first if available, then others.
	// This helps in establishing a somewhat predictable order if paths were involved.
	// With the current flat list from reflection, order of parsedConfigs matters less here.

	sortedParsedConfigs := make([]ParsedConfig, len(parsedConfigs))
	copy(sortedParsedConfigs, parsedConfigs)
	sort.Slice(sortedParsedConfigs, func(i, j int) bool {
		if sortedParsedConfigs[i].IsTopLevel {
			return true
		}
		if sortedParsedConfigs[j].IsTopLevel {
			return false
		}
		return sortedParsedConfigs[i].Name < sortedParsedConfigs[j].Name
	})

	for _, pc := range sortedParsedConfigs {
		for _, field := range pc.Fields {
			if field.EnvName != "" && !seenEnvVars[field.EnvName] {
				allFields = append(allFields, field)
				seenEnvVars[field.EnvName] = true
			}
		}
	}
	// Sort by EnvName for consistent table output
	sort.Slice(allFields, func(i, j int) bool {
		return allFields[i].EnvName < allFields[j].EnvName
	})
	return allFields
}

func formatRequiredText(reqStatus string) string {
	switch strings.ToUpper(reqStatus) {
	case "Y", "TRUE":
		return "Yes"
	case "N", "FALSE":
		return "No"
	case "C":
		return "Conditional"
	default:
		if reqStatus != "" {
			return reqStatus // Show as is if not recognized
		}
		return "No" // Default to No if empty
	}
}
