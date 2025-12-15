# protodump

Protodump is a utility to dump all Protobuf file descriptors from a given binary as *.proto files:

![Demo](https://raw.githubusercontent.com/zjx20/protodump/main/demo/demo.gif)

## Usage

```
go install github.com/zjx20/protodump/cmd/protodump@latest
./protodump -file <file to extract from> -output <output directory>
```

## Credits

This project is a fork of [arkadiyt/protodump](https://github.com/arkadiyt/protodump). Thanks to the original author for creating this useful tool.
