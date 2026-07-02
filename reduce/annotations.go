package reduce

import "github.com/realkarych/catacomb/model"

func (g *Graph) NodeBySourceKey(sourceKey string) *model.Node {
	for _, n := range g.Nodes {
		if model.NodeSourceKey(n.ID) == sourceKey {
			return n
		}
	}
	return nil
}

func (g *Graph) ApplyAnnotations(anns []model.Annotation) {
	byKey := map[string][]model.Annotation{}
	for _, a := range anns {
		byKey[a.SourceKey] = append(byKey[a.SourceKey], a)
	}
	for sourceKey, group := range byKey {
		n := g.NodeBySourceKey(sourceKey)
		if n == nil {
			continue
		}
		for _, a := range group {
			n.Annotations = model.SetAnnotation(n.Annotations, a.Owner, a.Key, a.Value)
		}
	}
}
