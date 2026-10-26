package service

import (
	"fmt"
	"math"
	"strings"

	"github.com/heinousness/aibi/internal/core/domain"
)

const maxTopValuesInDescribe = 10

// describe отдаёт модели профиль датасета из каталога. Запросов к базе
// он не делает: всё посчитано при загрузке.
func (r *agentRun) describe(name string) (string, int, error) {
	name = strings.TrimSpace(name)
	var targets []domain.Dataset
	for _, d := range r.datasets {
		if !d.Ready() {
			continue
		}
		if name == "" || strings.EqualFold(d.Name, name) || strings.EqualFold(d.TableName, name) {
			targets = append(targets, d)
		}
	}
	if len(targets) == 0 {
		return "", 0, fmt.Errorf("датасета %q нет в каталоге", name)
	}
	r.described = true

	out := make([]map[string]any, 0, len(targets))
	for _, d := range targets {
		out = append(out, r.describeDataset(d))
	}
	if len(out) == 1 {
		return encodeOutput(out[0]), 0, nil
	}
	return encodeOutput(map[string]any{"datasets": out}), 0, nil
}

func (r *agentRun) describeDataset(d domain.Dataset) map[string]any {
	origin := "загружен из файла " + d.SourceFile
	if d.Attached() {
		origin = "таблица подключённой базы «" + d.SourceName + "»"
	}

	cols := make([]map[string]any, 0, len(d.Columns))
	for _, c := range d.Columns {
		col := map[string]any{
			"name":  c.Name,
			"title": orDefault(c.SourceName, c.Name),
			"type":  string(c.Type),
			"role":  roleNames[c.Role],
		}
		if c.Comment != "" {
			col["comment"] = c.Comment
		}
		if p := c.Profile; p != nil {
			col["distinct"] = p.DistinctCount
			if share := p.NullShare(); share > 0 {
				col["nulls_pct"] = math.Round(share*1000) / 10
			}
			if p.Unique() {
				col["unique"] = true
			}
			if p.MinValue != nil {
				col["min"] = *p.MinValue
			}
			if p.MaxValue != nil {
				col["max"] = *p.MaxValue
			}
			if p.Mean != nil {
				col["mean"] = math.Round(*p.Mean*100) / 100
			}
			if len(p.TopValues) > 0 && !c.Type.IsTemporal() && c.Role != domain.RoleIdentifier {
				top := p.TopValues
				if len(top) > maxTopValuesInDescribe {
					top = top[:maxTopValuesInDescribe]
				}
				values := make([]map[string]any, 0, len(top))
				for _, v := range top {
					values = append(values, map[string]any{
						"value": compactCell(v.Value), "count": v.Count,
						"share_pct": math.Round(v.Share*1000) / 10,
					})
				}
				col["top_values"] = values
			}
		}
		cols = append(cols, col)
	}

	var links []map[string]any
	for _, rel := range r.relations {
		switch d.ID {
		case rel.LeftDatasetID:
			links = append(links, map[string]any{
				"column": rel.LeftColumn, "dataset": rel.RightDataset, "other_column": rel.RightColumn,
				"overlap_pct": math.Round(rel.Overlap * 100), "cardinality": rel.Cardinality,
			})
		case rel.RightDatasetID:
			links = append(links, map[string]any{
				"column": rel.RightColumn, "dataset": rel.LeftDataset, "other_column": rel.LeftColumn,
				"overlap_pct": math.Round(rel.Overlap * 100), "cardinality": rel.Cardinality,
			})
		}
	}

	out := map[string]any{
		"dataset": d.Name,
		"origin":  origin,
		"rows":    d.RowCount,
		"columns": cols,
	}
	if d.Description != "" {
		out["description"] = d.Description
	}
	if len(links) > 0 {
		out["relations"] = links
	}
	return out
}
