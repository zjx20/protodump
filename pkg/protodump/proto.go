package protodump

import (
	"fmt"
	"path"
	"reflect"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// CommentInfo holds the comments for a specific location
type CommentInfo struct {
	LeadingComments         string
	TrailingComments        string
	LeadingDetachedComments []string
}

// pathKey converts a path slice to a string key for map lookup
func pathKey(path []int32) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = strconv.Itoa(int(p))
	}
	return strings.Join(parts, ".")
}

type ProtoDefinition struct {
	builder     strings.Builder
	indendation int
	pb          *descriptorpb.FileDescriptorProto
	descriptor  protoreflect.FileDescriptor
	filename    string
	comments    map[string]*CommentInfo // path -> comments
}

// buildCommentMap extracts all comments from SourceCodeInfo and builds a lookup map
func (pd *ProtoDefinition) buildCommentMap() {
	pd.comments = make(map[string]*CommentInfo)

	sci := pd.pb.GetSourceCodeInfo()
	if sci == nil {
		return
	}

	for _, loc := range sci.GetLocation() {
		key := pathKey(loc.GetPath())
		pd.comments[key] = &CommentInfo{
			LeadingComments:         loc.GetLeadingComments(),
			TrailingComments:        loc.GetTrailingComments(),
			LeadingDetachedComments: loc.GetLeadingDetachedComments(),
		}
	}
}

// getComments returns the CommentInfo for the given path, or nil if none exists
func (pd *ProtoDefinition) getComments(path ...int32) *CommentInfo {
	if pd.comments == nil {
		return nil
	}
	return pd.comments[pathKey(path)]
}

// writeLeadingComments writes leading detached comments and leading comments
func (pd *ProtoDefinition) writeLeadingComments(path ...int32) {
	info := pd.getComments(path...)
	if info == nil {
		return
	}

	// Write leading detached comments (separated by blank lines)
	for _, comment := range info.LeadingDetachedComments {
		pd.writeComment(comment)
		pd.write("\n") // Extra blank line between detached comments
	}

	// Write leading comment
	if info.LeadingComments != "" {
		pd.writeComment(info.LeadingComments)
	}
}

// writeTrailingComment writes a trailing comment on the same line
func (pd *ProtoDefinition) writeTrailingComment(path ...int32) {
	info := pd.getComments(path...)
	if info == nil || info.TrailingComments == "" {
		return
	}

	// Trailing comments are typically on the same line
	comment := strings.TrimSpace(info.TrailingComments)
	if comment != "" {
		// Remove trailing newline and convert to single-line comment
		comment = strings.TrimSuffix(comment, "\n")
		if !strings.Contains(comment, "\n") {
			pd.write(" //")
			pd.write(comment)
		}
	}
}

// writeComment writes a comment block with proper indentation
func (pd *ProtoDefinition) writeComment(comment string) {
	if comment == "" {
		return
	}

	// Remove trailing newline
	comment = strings.TrimSuffix(comment, "\n")
	lines := strings.Split(comment, "\n")

	for _, line := range lines {
		pd.writeIndented("//")
		pd.write(line)
		pd.write("\n")
	}
}

func (pd *ProtoDefinition) indent() {
	pd.indendation += 1
}

func (pd *ProtoDefinition) dedent() {
	pd.indendation -= 1
}

func (pd *ProtoDefinition) writeIndented(s string) {
	pd.builder.WriteString(strings.Repeat("  ", pd.indendation))
	pd.write(s)
}

func (pd *ProtoDefinition) write(s string) {
	pd.builder.WriteString(s)
}

func (pd *ProtoDefinition) String() string {
	return pd.builder.String()
}

func (pd *ProtoDefinition) Filename() string {
	goPackage := pd.pb.GetOptions().GetGoPackage()
	index := strings.Index(goPackage, ";")
	if index == -1 {
		return pd.descriptor.Path()
	}

	return path.Join(goPackage[:index], path.Base(pd.descriptor.Path()))
}

func (pd *ProtoDefinition) writeMethodWithPath(method protoreflect.MethodDescriptor, servicePath []int32, methodIdx int) {
	methodPath := append(append([]int32{}, servicePath...), 2, int32(methodIdx)) // 2 = method field in ServiceDescriptorProto

	pd.writeLeadingComments(methodPath...)
	pd.writeIndented("rpc ")
	pd.write(string(method.Name()))
	pd.write(" (")
	if method.IsStreamingClient() {
		pd.write("stream ")
	}
	pd.write(".")
	pd.write(string(method.Input().FullName()))
	pd.write(") returns (")
	if method.IsStreamingServer() {
		pd.write("stream ")
	}
	pd.write(".")
	pd.write(string(method.Output().FullName()))
	pd.write(") {}")
	pd.writeTrailingComment(methodPath...)
	pd.write("\n")
}

func (pd *ProtoDefinition) writeMethod(method protoreflect.MethodDescriptor) {
	// Legacy method without path tracking
	pd.writeIndented("rpc ")
	pd.write(string(method.Name()))
	pd.write(" (")
	if method.IsStreamingClient() {
		pd.write("stream ")
	}
	pd.write(".")
	pd.write(string(method.Input().FullName()))
	pd.write(") returns (")
	if method.IsStreamingServer() {
		pd.write("stream ")
	}
	pd.write(".")
	pd.write(string(method.Output().FullName()))
	pd.write(") {}\n")
}

func (pd *ProtoDefinition) writeServiceWithPath(service protoreflect.ServiceDescriptor, serviceIdx int) {
	servicePath := []int32{6, int32(serviceIdx)} // 6 = service field in FileDescriptorProto

	pd.writeLeadingComments(servicePath...)
	pd.write("service ")
	pd.write(string(service.Name()))
	pd.write(" {\n")
	pd.indent()
	for i := 0; i < service.Methods().Len(); i++ {
		pd.writeMethodWithPath(service.Methods().Get(i), servicePath, i)
	}
	pd.dedent()
	pd.writeIndented("}\n\n")
}

func (pd *ProtoDefinition) writeService(service protoreflect.ServiceDescriptor) {
	// Legacy method without path tracking
	pd.write("service ")
	pd.write(string(service.Name()))
	pd.write(" {\n")
	pd.indent()
	for i := 0; i < service.Methods().Len(); i++ {
		pd.writeMethod(service.Methods().Get(i))
	}
	pd.dedent()
	pd.writeIndented("}\n\n")
}

func (pd *ProtoDefinition) writeType(field protoreflect.FieldDescriptor) {
	kind := field.Kind().String()

	if kind == "message" {
		pd.write(".")
		pd.write(string(field.Message().FullName()))
	} else if kind == "enum" {
		pd.write(".")
		pd.write(string(field.Enum().FullName()))
	} else if kind == "map" {
		pd.write("map<")
		pd.writeType(field.MapKey())
		pd.write(", ")
		pd.writeType(field.MapValue())
		pd.write(">")
	} else {
		pd.write(kind)
	}
}

func (pd *ProtoDefinition) writeOneofWithPath(oneof protoreflect.OneofDescriptor, msgPath []int32, oneofIdx int, fieldIndexMap map[string]int) {
	oneofPath := append(append([]int32{}, msgPath...), 8, int32(oneofIdx)) // 8 = oneof_decl field in DescriptorProto

	if oneof.IsSynthetic() {
		// For synthetic oneofs (optional fields in proto3), just write the field
		field := oneof.Fields().Get(0)
		fieldIdx := fieldIndexMap[string(field.Name())]
		pd.writeFieldWithPath(field, msgPath, fieldIdx)
	} else {
		pd.writeLeadingComments(oneofPath...)
		pd.writeIndented("")
		pd.write("oneof ")
		pd.write(string(oneof.Name()))
		pd.write(" {\n")
		pd.indent()
		for i := 0; i < oneof.Fields().Len(); i++ {
			field := oneof.Fields().Get(i)
			fieldIdx := fieldIndexMap[string(field.Name())]
			pd.writeFieldWithPath(field, msgPath, fieldIdx)
		}
		pd.dedent()
		pd.writeIndented("}")
		pd.writeTrailingComment(oneofPath...)
		pd.write("\n")
	}
}

func (pd *ProtoDefinition) writeOneof(oneof protoreflect.OneofDescriptor) {
	// Legacy method without path tracking
	if oneof.IsSynthetic() {
		pd.writeField(oneof.Fields().Get(0))
	} else {
		pd.writeIndented("")
		pd.write("oneof ")
		pd.write(string(oneof.Name()))
		pd.write(" {\n")
		pd.indent()
		for i := 0; i < oneof.Fields().Len(); i++ {
			pd.writeField(oneof.Fields().Get(i))
		}
		pd.dedent()
		pd.writeIndented("}\n")
	}
}

func (pd *ProtoDefinition) writeFieldWithPath(field protoreflect.FieldDescriptor, msgPath []int32, fieldIdx int) {
	fieldPath := append(append([]int32{}, msgPath...), 2, int32(fieldIdx)) // 2 = field in DescriptorProto

	pd.writeLeadingComments(fieldPath...)
	pd.writeIndented("")
	if field.HasOptionalKeyword() {
		pd.write("optional ")
	} else if field.Cardinality().String() == "repeated" {
		pd.write("repeated ")
	} else if field.Cardinality().String() == "required" && pd.descriptor.Syntax().String() == "proto2" {
		pd.write("required ")
	}
	pd.writeType(field)
	pd.write(" ")
	pd.write(string(field.Name()))
	pd.write(" = ")
	pd.write(strconv.Itoa(int(field.Number())))
	if field.HasDefault() {
		pd.write(" [default = ")
		kind := field.Kind().String()
		if kind == "string" {
			pd.write(fmt.Sprintf("\"%s\"", field.Default().String()))
		} else if kind == "enum" {
			pd.write(string(field.DefaultEnumValue().Name()))
		} else {
			pd.write(field.Default().String())
		}

		pd.write("]")
	}
	pd.write(";")
	pd.writeTrailingComment(fieldPath...)
	pd.write("\n")
}

func (pd *ProtoDefinition) writeField(field protoreflect.FieldDescriptor) {
	// Legacy method without path tracking
	pd.writeIndented("")
	if field.HasOptionalKeyword() {
		pd.write("optional ")
	} else if field.Cardinality().String() == "repeated" {
		pd.write("repeated ")
	} else if field.Cardinality().String() == "required" && pd.descriptor.Syntax().String() == "proto2" {
		pd.write("required ")
	}
	pd.writeType(field)
	pd.write(" ")
	pd.write(string(field.Name()))
	pd.write(" = ")
	pd.write(strconv.Itoa(int(field.Number())))
	if field.HasDefault() {
		pd.write(" [default = ")
		kind := field.Kind().String()
		if kind == "string" {
			pd.write(fmt.Sprintf("\"%s\"", field.Default().String()))
		} else if kind == "enum" {
			pd.write(string(field.DefaultEnumValue().Name()))
		} else {
			pd.write(field.Default().String())
		}

		pd.write("]")
	}
	pd.write(";\n")
}

func (pd *ProtoDefinition) writeEnumWithPath(enum protoreflect.EnumDescriptor, basePath []int32, enumIdx int, isNested bool) {
	var enumPath []int32
	if isNested {
		// 4 = enum_type field in DescriptorProto (nested enum)
		enumPath = append(append([]int32{}, basePath...), 4, int32(enumIdx))
	} else {
		// 5 = enum_type field in FileDescriptorProto (top-level enum)
		enumPath = []int32{5, int32(enumIdx)}
	}

	pd.writeLeadingComments(enumPath...)
	pd.writeIndented("enum ")
	pd.write(string(enum.Name()))
	pd.write(" {\n")
	pd.indent()
	for i := 0; i < enum.Values().Len(); i++ {
		value := enum.Values().Get(i)
		valuePath := append(append([]int32{}, enumPath...), 2, int32(i)) // 2 = value field in EnumDescriptorProto

		pd.writeLeadingComments(valuePath...)
		pd.writeIndented(string(value.Name()))
		pd.write(" = ")
		pd.write(fmt.Sprintf("%d", value.Number()))
		pd.write(";")
		pd.writeTrailingComment(valuePath...)
		pd.write("\n")
	}
	pd.dedent()
	pd.writeIndented("}")
	pd.writeTrailingComment(enumPath...)
	pd.write("\n\n")
}

func (pd *ProtoDefinition) writeEnum(enum protoreflect.EnumDescriptor) {
	// Legacy method without path tracking
	pd.writeIndented("enum ")
	pd.write(string(enum.Name()))
	pd.write(" {\n")
	pd.indent()
	for i := 0; i < enum.Values().Len(); i++ {
		value := enum.Values().Get(i)
		pd.writeIndented(string(value.Name()))
		pd.write(" = ")
		pd.write(fmt.Sprintf("%d", value.Number()))
		pd.write(";\n")
	}
	pd.dedent()
	pd.writeIndented("}\n\n")
}

func (pd *ProtoDefinition) writeMessageWithPath(message protoreflect.MessageDescriptor, basePath []int32, msgIdx int, isNested bool) {
	var msgPath []int32
	if isNested {
		// 3 = nested_type field in DescriptorProto
		msgPath = append(append([]int32{}, basePath...), 3, int32(msgIdx))
	} else {
		// 4 = message_type field in FileDescriptorProto
		msgPath = []int32{4, int32(msgIdx)}
	}

	pd.writeLeadingComments(msgPath...)
	pd.writeIndented("message ")
	pd.write(string(message.Name()))
	pd.write(" {\n")
	pd.indent()

	for i := 0; i < message.ReservedNames().Len(); i++ {
		name := message.ReservedNames().Get(i)
		pd.writeIndented("reserved \"")
		pd.write(string(name))
		pd.write("\";\n")
	}

	for i := 0; i < message.ReservedRanges().Len(); i++ {
		pd.writeIndented("reserved ")
		reservedRange := message.ReservedRanges().Get(i)
		if reservedRange[0] > reservedRange[1] {
			reservedRange[1], reservedRange[0] = reservedRange[0], reservedRange[1]
		}
		reservedRange[1] -= 1
		if reservedRange[0] == reservedRange[1] {
			pd.write(fmt.Sprintf("%d", reservedRange[0]))
		} else {
			pd.write(fmt.Sprintf("%d", reservedRange[0]))
			pd.write(" to ")
			if reservedRange[1] == protowire.MaxValidNumber {
				pd.write("max")
			} else {
				pd.write(fmt.Sprintf("%d", reservedRange[1]))
			}
		}
		pd.write(";\n")
	}

	// Write nested messages
	for i := 0; i < message.Messages().Len(); i++ {
		pd.writeMessageWithPath(message.Messages().Get(i), msgPath, i, true)
	}

	// Write nested enums
	for i := 0; i < message.Enums().Len(); i++ {
		pd.writeEnumWithPath(message.Enums().Get(i), msgPath, i, true)
	}

	// Build field index map for oneof fields
	// The field index in SourceCodeInfo is based on the order in the proto definition,
	// which matches the order in the DescriptorProto's field list
	fieldIndexMap := make(map[string]int)
	for i := 0; i < message.Fields().Len(); i++ {
		field := message.Fields().Get(i)
		fieldIndexMap[string(field.Name())] = i
	}

	// Write non-oneof fields
	for i := 0; i < message.Fields().Len(); i++ {
		field := message.Fields().Get(i)
		if field.ContainingOneof() == nil {
			pd.writeFieldWithPath(field, msgPath, i)
		}
	}

	// Write oneofs (which include their fields)
	for i := 0; i < message.Oneofs().Len(); i++ {
		pd.writeOneofWithPath(message.Oneofs().Get(i), msgPath, i, fieldIndexMap)
	}

	pd.dedent()
	pd.writeIndented("}")
	pd.writeTrailingComment(msgPath...)
	pd.write("\n\n")
}

func (pd *ProtoDefinition) writeMessage(message protoreflect.MessageDescriptor) {
	// Legacy method without path tracking
	pd.writeIndented("message ")
	pd.write(string(message.Name()))
	pd.write(" {\n")
	pd.indent()

	for i := 0; i < message.ReservedNames().Len(); i++ {
		name := message.ReservedNames().Get(i)
		pd.writeIndented("reserved \"")
		pd.write(string(name))
		pd.write("\";\n")
	}

	for i := 0; i < message.ReservedRanges().Len(); i++ {
		pd.writeIndented("reserved ")
		reservedRange := message.ReservedRanges().Get(i)
		if reservedRange[0] > reservedRange[1] {
			reservedRange[1], reservedRange[0] = reservedRange[0], reservedRange[1]
		}
		reservedRange[1] -= 1
		if reservedRange[0] == reservedRange[1] {
			pd.write(fmt.Sprintf("%d", reservedRange[0]))
		} else {
			pd.write(fmt.Sprintf("%d", reservedRange[0]))
			pd.write(" to ")
			if reservedRange[1] == protowire.MaxValidNumber {
				pd.write("max")
			} else {
				pd.write(fmt.Sprintf("%d", reservedRange[1]))
			}
		}
		pd.write(";\n")
	}

	for i := 0; i < message.Messages().Len(); i++ {
		pd.writeMessage(message.Messages().Get(i))
	}

	for i := 0; i < message.Enums().Len(); i++ {
		pd.writeEnum(message.Enums().Get(i))
	}

	for i := 0; i < message.Fields().Len(); i++ {
		field := message.Fields().Get(i)
		if field.ContainingOneof() == nil {
			pd.writeField(field)
		}
	}

	for i := 0; i < message.Oneofs().Len(); i++ {
		pd.writeOneof(message.Oneofs().Get(i))
	}
	pd.dedent()
	pd.writeIndented("}\n\n")
}

func (pd *ProtoDefinition) writeImport(fileImport protoreflect.FileImport) {
	pd.write("import ")
	if fileImport.IsPublic {
		pd.write("public ")
	}
	pd.write("\"")
	pd.write(fileImport.Path())
	pd.write("\";\n")
}

func (pd *ProtoDefinition) writeStringFileOptions(name string, value string) {
	pd.write("option ")
	pd.write(name)
	pd.write(" = \"")
	pd.write(strings.ReplaceAll(value, "\\", "\\\\"))
	pd.write("\";\n")
}

func (pd *ProtoDefinition) writeBoolFileOptions(name string, value bool) {
	pd.write("option ")
	pd.write(name)
	pd.write(" = ")
	pd.write(strconv.FormatBool(value))
	pd.write(";\n")
}

func (pd *ProtoDefinition) writeFileOptions() {
	optionDefinitions := []struct {
		OptionName string
		FieldName  string
	}{
		{"java_package", "JavaPackage"},
		{"java_outer_classname", "JavaOuterClassname"},
		{"java_multiple_files", "JavaMultipleFiles"},
		{"java_string_check_utf8", "JavaStringCheckUtf8"},
		// TODO OptimizeMode: https://github.com/protocolbuffers/protobuf/blob/main/src/google/protobuf/descriptor.proto#L384
		{"go_package", "GoPackage"},
		// TODO generic services: https://github.com/protocolbuffers/protobuf/blob/main/src/google/protobuf/descriptor.proto#L403
		// TODO deprecated: https://github.com/protocolbuffers/protobuf/blob/main/src/google/protobuf/descriptor.proto#L412
		{"cc_enable_arenas", "CcEnableArenas"},
		{"objc_class_prefix", "ObjcClassPrefix"},
		{"csharp_namespace", "CsharpNamespace"},
		{"swift_prefix", "SwiftPrefix"},
		{"php_class_prefix", "PhpClassPrefix"},
		{"php_namespace", "PhpNamespace"},
		{"php_metadata_namespace", "PhpMetadataNamespace"},
		{"ruby_package", "RubyPackage"},
	}

	optionsPtr := reflect.ValueOf(pd.pb.GetOptions())
	if optionsPtr.IsNil() {
		return
	}
	options := optionsPtr.Elem()
	printedOption := false
	for _, option := range optionDefinitions {
		elemPtr := options.FieldByName(option.FieldName)
		if !elemPtr.IsNil() {
			elem := elemPtr.Elem()
			kind := elem.Kind()
			if kind == reflect.String {
				pd.writeStringFileOptions(option.OptionName, elem.String())
			} else if kind == reflect.Bool {
				pd.writeBoolFileOptions(option.OptionName, elem.Bool())
			}
			printedOption = true
		}
	}

	if printedOption {
		pd.write("\n")
	}
}

func (pd *ProtoDefinition) writeFileDescriptor() {
	// Write file-level leading comment (attached to syntax)
	pd.writeLeadingComments(12) // 12 = syntax field in FileDescriptorProto

	pd.write("syntax = \"")
	pd.write(pd.descriptor.Syntax().String())
	pd.write("\";\n\n")

	packageName := pd.descriptor.FullName()
	if packageName != "" {
		pd.write("package ")
		pd.write(string(packageName))
		pd.write(";\n\n")
	}

	pd.writeFileOptions()

	for i := 0; i < pd.descriptor.Imports().Len(); i++ {
		pd.writeImport(pd.descriptor.Imports().Get(i))
	}

	if pd.descriptor.Imports().Len() > 0 {
		pd.write("\n")
	}

	for i := 0; i < pd.descriptor.Services().Len(); i++ {
		pd.writeServiceWithPath(pd.descriptor.Services().Get(i), i)
	}

	for i := 0; i < pd.descriptor.Messages().Len(); i++ {
		pd.writeMessageWithPath(pd.descriptor.Messages().Get(i), nil, i, false)
	}

	for i := 0; i < pd.descriptor.Enums().Len(); i++ {
		pd.writeEnumWithPath(pd.descriptor.Enums().Get(i), nil, i, false)
	}
}

func NewFromBytes(payload []byte) (*ProtoDefinition, error) {
	var pb descriptorpb.FileDescriptorProto
	err := proto.Unmarshal(payload, &pb)
	if err != nil {
		return nil, fmt.Errorf("Couldn't unmarshal proto: %w", err)
	}

	return NewFromDescriptor(&pb)
}

func NewFromDescriptor(pb *descriptorpb.FileDescriptorProto) (*ProtoDefinition, error) {
	fileOptions := protodesc.FileOptions{AllowUnresolvable: true}
	descriptor, err := fileOptions.New(pb, &protoregistry.Files{})

	if err != nil {
		return nil, fmt.Errorf("Couldn't create FileDescriptor: %w", err)
	}

	pd := ProtoDefinition{
		pb:         pb,
		descriptor: descriptor,
	}

	// Build comment map from SourceCodeInfo
	pd.buildCommentMap()

	pd.writeFileDescriptor()

	return &pd, nil

}
