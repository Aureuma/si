package main

import (
	"fmt"
	"strings"
)

// renderAlignedTable renders a fixed-width text table using displayWidth-based
// cell measurement so each column starts at a stable offset.
func renderAlignedTable(headers []string, rows [][]string, gutter int) []string {
	if len(headers) == 0 {
		return nil
	}
	if gutter < 1 {
		gutter = 1
	}
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = displayWidth(header)
	}
	for _, row := range rows {
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			if w := displayWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}
	sep := strings.Repeat(" ", gutter)
	out := make([]string, 0, len(rows)+1)
	out = append(out, renderAlignedTableRow(headers, widths, sep))
	for _, row := range rows {
		out = append(out, renderAlignedTableRow(row, widths, sep))
	}
	return out
}

func printAlignedTable(headers []string, rows [][]string, gutter int) {
	for _, line := range renderAlignedTable(headers, rows, gutter) {
		fmt.Println(line)
	}
}

func printKeyValueTable(rows [][2]string) {
	if len(rows) == 0 {
		return
	}
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{row[0], row[1]})
	}
	for _, line := range renderAlignedTable([]string{"", ""}, tableRows, 1) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Println(line)
	}
}

func renderAlignedTableRow(row []string, widths []int, sep string) string {
	cells := make([]string, len(widths))
	for i, width := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		cells[i] = padRightANSI(cell, width)
	}
	return strings.Join(cells, sep)
}
