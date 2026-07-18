package main

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"
)

const officePreviewWindow = 3000

func officeDocumentExtensions() []string {
	return []string{
		".doc", ".dot", ".docx", ".docm", ".dotx", ".dotm",
		".xls", ".xlt", ".xlsx", ".xlsm", ".xltx", ".xltm",
		".ppt", ".pps", ".pot", ".pptx", ".pptm", ".ppsx", ".ppsm", ".potx", ".potm",
	}
}

func isModernOfficeExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".docx", ".docm", ".dotx", ".dotm",
		".xlsx", ".xlsm", ".xltx", ".xltm",
		".pptx", ".pptm", ".ppsx", ".ppsm", ".potx", ".potm":
		return true
	default:
		return false
	}
}

func scanOfficeContent(ctx context.Context, filePath string, ext string, needle string, caseSensitive bool) (bool, string, error) {
	reader, err := zip.OpenReader(filePath)
	if err != nil {
		return false, "", err
	}
	defer reader.Close()

	var firstErr error
	for _, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return false, "", err
		}

		name := path.Clean(strings.ReplaceAll(file.Name, "\\", "/"))
		if !shouldScanOfficePart(ext, name) {
			continue
		}

		part, err := file.Open()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		matched, preview, scanErr := scanXMLText(ctx, part, needle, caseSensitive)
		_ = part.Close()
		if matched {
			return true, fmt.Sprintf("%s: %s", officePartLabel(name), preview), nil
		}
		if scanErr != nil && firstErr == nil {
			firstErr = scanErr
		}
	}

	return false, "", firstErr
}

func shouldScanOfficePart(ext string, name string) bool {
	if !strings.HasSuffix(name, ".xml") {
		return false
	}

	switch strings.ToLower(ext) {
	case ".docx", ".docm", ".dotx", ".dotm":
		return name == "word/document.xml" ||
			strings.HasPrefix(name, "word/header") ||
			strings.HasPrefix(name, "word/footer") ||
			strings.HasPrefix(name, "word/comments") ||
			name == "word/footnotes.xml" ||
			name == "word/endnotes.xml"
	case ".xlsx", ".xlsm", ".xltx", ".xltm":
		return name == "xl/sharedStrings.xml" ||
			strings.HasPrefix(name, "xl/worksheets/") ||
			strings.HasPrefix(name, "xl/comments") ||
			strings.HasPrefix(name, "xl/threadedComments") ||
			strings.HasPrefix(name, "xl/drawings/")
	case ".pptx", ".pptm", ".ppsx", ".ppsm", ".potx", ".potm":
		return strings.HasPrefix(name, "ppt/slides/") ||
			strings.HasPrefix(name, "ppt/notesSlides/") ||
			strings.HasPrefix(name, "ppt/comments/")
	default:
		return false
	}
}

func scanXMLText(ctx context.Context, reader io.Reader, needle string, caseSensitive bool) (bool, string, error) {
	decoder := xml.NewDecoder(reader)
	var buffer strings.Builder

	for {
		if err := ctx.Err(); err != nil {
			return false, "", err
		}

		token, err := decoder.Token()
		if err == io.EOF {
			return false, "", nil
		}
		if err != nil {
			return false, "", err
		}

		charData, ok := token.(xml.CharData)
		if !ok {
			continue
		}

		text := strings.Join(strings.Fields(string(charData)), " ")
		if text == "" {
			continue
		}
		if buffer.Len() > 0 {
			buffer.WriteByte(' ')
		}
		buffer.WriteString(text)

		segment := buffer.String()
		candidate := segment
		if !caseSensitive {
			candidate = strings.ToLower(candidate)
		}
		if strings.Contains(candidate, needle) {
			return true, compactMatch(segment, needle, caseSensitive, 220), nil
		}
		if len([]rune(segment)) > officePreviewWindow {
			buffer.Reset()
			buffer.WriteString(tailRunes(segment, officePreviewWindow/2))
		}
	}
}

func tailRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[len(runes)-limit:])
}

func officePartLabel(name string) string {
	switch {
	case strings.HasPrefix(name, "word/"):
		return "Word"
	case strings.HasPrefix(name, "xl/"):
		return "Excel"
	case strings.HasPrefix(name, "ppt/"):
		return "PowerPoint"
	default:
		return "Office"
	}
}
