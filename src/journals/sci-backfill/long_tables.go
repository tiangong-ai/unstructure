package main

import (
	"bytes"
	"fmt"
	"html"
	"strconv"
	"strings"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// splitLongTables follows the old loader's long-block branch: select HTML
// tables, keep short tables whole, and split large tables by row.
func splitLongTables(block string, count func(string) (int, error), limit int) ([]string, error) {
	context := &nethtml.Node{Type: nethtml.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := nethtml.ParseFragment(strings.NewReader(block), context)
	if err != nil {
		return nil, err
	}
	var chunks []string
	var visit func(*nethtml.Node) error
	visit = func(node *nethtml.Node) error {
		if node.DataAtom == atom.Table {
			var out bytes.Buffer
			if err := nethtml.Render(&out, node); err != nil {
				return err
			}
			n, err := count(out.String())
			if err != nil {
				return err
			}
			if n < limit {
				chunks = append(chunks, out.String())
			} else {
				parts, err := splitTableRows(node, count, limit)
				if err != nil {
					return err
				}
				chunks = append(chunks, parts...)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, node := range nodes {
		if err := visit(node); err != nil {
			return nil, err
		}
	}
	return chunks, nil
}

func splitTableRows(table *nethtml.Node, count func(string) (int, error), limit int) ([]string, error) {
	var headers [][]string
	var rows [][]string
	width := 0
	for section := table.FirstChild; section != nil; section = section.NextSibling {
		if section.DataAtom != atom.Thead && section.DataAtom != atom.Tbody && section.DataAtom != atom.Tfoot {
			continue
		}
		spans := map[int]tableSpan{}
		for row := section.FirstChild; row != nil; row = row.NextSibling {
			if row.DataAtom != atom.Tr {
				continue
			}
			cells, isHeader, err := readTableRow(row, spans)
			if err != nil {
				return nil, err
			}
			width = max(width, len(cells))
			if section.DataAtom == atom.Thead || (len(rows) == 0 && isHeader) {
				headers = append(headers, cells)
			} else {
				rows = append(rows, cells)
			}
		}
	}
	if len(headers) == 0 {
		header := make([]string, width)
		for i := range header {
			header[i] = strconv.Itoa(i)
		}
		headers = append(headers, header)
	}
	normalizeNumericColumns(rows, width)
	var chunks []string
	total, start := 0, 0
	for i, row := range rows {
		n, err := count(renderTable(headers, [][]string{row}, width, true))
		if err != nil {
			return nil, err
		}
		if total+n > limit && i > start {
			chunks = append(chunks, renderTable(headers, rows[start:i], width, false))
			total, start = 0, i
		}
		total += n
	}
	if start < len(rows) {
		chunks = append(chunks, renderTable(headers, rows[start:], width, false))
	}
	return chunks, nil
}

type tableSpan struct {
	text string
	left int
}

func readTableRow(row *nethtml.Node, spans map[int]tableSpan) ([]string, bool, error) {
	var cells []string
	allHeaders := true
	lastSpan := -1
	for col := range spans {
		lastSpan = max(lastSpan, col)
	}
	appendSpans := func() {
		for {
			col := len(cells)
			span, ok := spans[col]
			if !ok {
				return
			}
			cells = append(cells, span.text)
			if span.left == 1 {
				delete(spans, col)
			} else {
				span.left--
				spans[col] = span
			}
		}
	}
	for cell := row.FirstChild; cell != nil; cell = cell.NextSibling {
		if cell.DataAtom != atom.Th && cell.DataAtom != atom.Td {
			continue
		}
		allHeaders = allHeaders && cell.DataAtom == atom.Th
		rowspan, colspan := 1, 1
		for _, attr := range cell.Attr {
			if attr.Key != "rowspan" && attr.Key != "colspan" {
				continue
			}
			n, err := strconv.Atoi(attr.Val)
			if err != nil || n < 1 || n > 1000 {
				return nil, false, fmt.Errorf("invalid HTML table %s=%q", attr.Key, attr.Val)
			}
			if attr.Key == "rowspan" {
				rowspan = n
			} else {
				colspan = n
			}
		}
		var text strings.Builder
		var visit func(*nethtml.Node)
		visit = func(n *nethtml.Node) {
			if n.Type == nethtml.TextNode {
				text.WriteString(n.Data)
			}
			if n.DataAtom == atom.Br {
				text.WriteByte(' ')
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				visit(child)
			}
		}
		visit(cell)
		value := strings.Join(strings.Fields(text.String()), " ")
		if value == "" {
			value = "NaN"
		}
		for range colspan {
			appendSpans()
			col := len(cells)
			cells = append(cells, value)
			if rowspan > 1 {
				spans[col] = tableSpan{text: value, left: rowspan - 1}
			}
		}
	}
	for len(cells) <= lastSpan {
		col := len(cells)
		appendSpans()
		if len(cells) == col {
			cells = append(cells, "NaN")
		}
	}
	return cells, len(cells) > 0 && allHeaders, nil
}

func normalizeNumericColumns(rows [][]string, width int) {
	for col := 0; col < width; col++ {
		numeric, decimal := true, false
		for _, row := range rows {
			if col >= len(row) || row[col] == "NaN" {
				decimal = true
				continue
			}
			value := strings.ReplaceAll(row[col], ",", "")
			if _, err := strconv.ParseInt(value, 10, 64); err != nil {
				if _, err := strconv.ParseFloat(value, 64); err != nil {
					numeric = false
					break
				}
				decimal = true
			}
		}
		if !numeric {
			continue
		}
		for _, row := range rows {
			if col >= len(row) || row[col] == "NaN" {
				continue
			}
			value := strings.ReplaceAll(row[col], ",", "")
			if decimal {
				number, _ := strconv.ParseFloat(value, 64)
				value = strconv.FormatFloat(number, 'g', -1, 64)
				if !strings.ContainsAny(value, ".eE") {
					value += ".0"
				}
			} else {
				number, _ := strconv.ParseInt(value, 10, 64)
				value = strconv.FormatInt(number, 10)
			}
			row[col] = value
		}
	}
}

// pandas to_html formats each group with its column names and no row index.
func renderTable(headers [][]string, rows [][]string, width int, indented bool) string {
	var out strings.Builder
	out.WriteString("<table class=\"dataframe\">\n  <thead>\n")
	for level, header := range headers {
		if len(headers) == 1 {
			out.WriteString("    <tr style=\"text-align: right;\">\n")
		} else {
			out.WriteString("    <tr>\n")
		}
		for i := 0; i < width; i++ {
			value := fmt.Sprintf("Unnamed: %d", i)
			if len(headers) > 1 {
				value += fmt.Sprintf("_level_%d", level)
			}
			if i < len(header) && header[i] != "NaN" {
				value = header[i]
			}
			end := i + 1
			if level < len(headers)-1 && i < len(header) && header[i] != "NaN" {
				for end < len(header) && header[end] == header[i] {
					end++
				}
			}
			if end-i > 1 {
				fmt.Fprintf(&out, "      <th colspan=\"%d\" halign=\"left\">%s</th>\n", end-i, escapeTableText(value))
				i = end - 1
			} else {
				fmt.Fprintf(&out, "      <th>%s</th>\n", escapeTableText(value))
			}
		}
		out.WriteString("    </tr>\n")
	}
	out.WriteString("  </thead>\n  <tbody>\n")
	for _, row := range rows {
		out.WriteString("    <tr>\n")
		for i := 0; i < width; i++ {
			value := "NaN"
			if i < len(row) {
				value = row[i]
			}
			fmt.Fprintf(&out, "      <td>%s</td>\n", escapeTableText(value))
		}
		out.WriteString("    </tr>\n")
	}
	out.WriteString("  </tbody>\n</table>")
	result := out.String()
	if !indented {
		lines := strings.Split(result, "\n")
		for i, line := range lines {
			lines[i] = strings.TrimLeft(line, " ")
		}
		return strings.Join(lines, "\n")
	}
	return result
}

func escapeTableText(value string) string {
	return strings.NewReplacer("&#34;", "\"", "&#39;", "'").Replace(html.EscapeString(value))
}
