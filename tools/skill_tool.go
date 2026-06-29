package tools

import (
	"context"
	"fmt"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/skills"
)

type skillCatalog interface {
	Skills() []skills.Skill
}

type skillHarnessCellCatalog struct {
	cell *SkillHarnessCell
}

func (catalog skillHarnessCellCatalog) Skills() []skills.Skill {
	return catalog.cell.Skills()
}

func (catalog skillHarnessCellCatalog) ReloadSkillsFromDisk(ctx context.Context) (skills.LoadOutput, error) {
	return catalog.cell.ReloadSkillsFromDisk(ctx)
}

type SkillTool = agent.SkillTool
type SkillHarnessCell = agent.SkillHarnessCell

func NewSkillHarnessCell() *SkillHarnessCell {
	return agent.NewSkillHarnessCell()
}

func NewSkillTool(available []skills.Skill) SkillTool {
	return agent.NewSkillTool(available)
}

func NewSkillToolFromHarnessCell(cell *SkillHarnessCell) SkillTool {
	return agent.NewSkillToolFromHarnessCell(cell)
}

func NewCatalogSkillTool(catalog *skills.Catalog) SkillTool {
	return agent.NewCatalogSkillTool(catalog)
}

func catalogFromSkillHarnessCell(cell *SkillHarnessCell) skillCatalog {
	if cell == nil || !cell.IsSet() {
		return nil
	}
	if catalog, ok := cell.Provider().(*skills.Catalog); ok {
		return catalog
	}
	return skillHarnessCellCatalog{cell: cell}
}

func reloadSkillCatalog(ctx context.Context, catalog skillCatalog) (skills.LoadOutput, error) {
	if catalog == nil {
		return skills.LoadOutput{}, fmt.Errorf("skill catalog is not initialized")
	}
	if reloader, ok := catalog.(interface{ Reload() (skills.LoadOutput, error) }); ok {
		return reloader.Reload()
	}
	if reloader, ok := catalog.(interface {
		ReloadSkillsFromDisk(context.Context) (skills.LoadOutput, error)
	}); ok {
		return reloader.ReloadSkillsFromDisk(ctx)
	}
	return skills.LoadOutput{Skills: catalog.Skills()}, nil
}
