// Package render превращает Markdown статьи в HTML.
package render

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// Typographer не подключаем: он меняет кавычки и дефисы, а текст статьи уже набран как надо.
// Сырой HTML разрешён: статьи пишет автор репозитория, а <details> и острова графиков без него не собрать.
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

// Markdown рендерит текст статьи в HTML.
func Markdown(src []byte) ([]byte, error) {
	var buf bytes.Buffer
	ctx := parser.NewContext(parser.WithIDs(newIDs()))
	if err := md.Convert(src, &buf, parser.WithContext(ctx)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
