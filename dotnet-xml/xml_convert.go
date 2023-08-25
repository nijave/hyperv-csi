package dotnet_xml

import (
	"regexp"
	"strconv"
	"strings"
)

// https://github.com/microsoft/referencesource/blob/master/System.Xml/System/Xml/XmlConvert.cs

var decodeCharPattern = regexp.MustCompile(`_[Xx]([0-9a-fA-F]{4}|[0-9a-fA-F]{8})_`)

func DecodeName(name string) string {
	if name == "" || strings.Index(name, "_") < 0 {
		return name
	}

	matches := decodeCharPattern.FindAllStringSubmatchIndex(name, -1)
	if len(matches) == 0 {
		return name
	}

	var bufBld strings.Builder
	copyPosition := 0
	for i, pos := range matches {
		bufBld.WriteString(name[copyPosition:pos[0]])
		copyPosition = matches[i][1]

		u64, err := strconv.ParseInt(name[pos[2]:pos[3]], 16, 32)
		if err != nil {
			bufBld.WriteString(name[pos[2]:pos[3]])
		} else {
			bufBld.WriteRune(rune(int(u64)))
		}

	}

	if copyPosition < len(name) {
		bufBld.WriteString(name[copyPosition:])
	}

	return bufBld.String()
}
