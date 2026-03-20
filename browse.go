package iec61850

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"sort"

	"github.com/otfabric/go-mms"

	"github.com/otfabric/go-iec61850/internal/mapping"
)

// ListLogicalDevices returns the logical devices (MMS domains)
// available on the server.
//
// Results are sorted alphabetically by name.
func (c *Client) ListLogicalDevices(ctx context.Context) ([]LogicalDevice, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}

	lds, err := c.cachedLDs(ctx)
	if err != nil {
		return nil, err
	}

	c.logger.Debug("iec61850: listed logical devices", "count", len(lds))
	return lds, nil
}

// ListLogicalNodes returns the logical nodes within the specified
// logical device.
//
// The logical nodes are discovered by listing all named variables in
// the MMS domain and extracting the unique first $-delimited segment
// (the LN name in IEC 61850 MMS mapping).
//
// Results are sorted alphabetically by name.
func (c *Client) ListLogicalNodes(ctx context.Context, ld string) ([]LogicalNode, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}

	if ld == "" {
		return nil, &ReferenceError{Input: ld, Reason: "empty logical device name"}
	}

	items, err := c.cachedItems(ctx, ld)
	if err != nil {
		return nil, fmt.Errorf("iec61850: list logical nodes in %q: %w", ld, err)
	}

	lnNames := mapping.ExtractLogicalNodes(items)
	nodes := make([]LogicalNode, len(lnNames))
	for i, name := range lnNames {
		nodes[i] = LogicalNode{Name: name, LD: ld}
	}

	c.logger.Debug("iec61850: listed logical nodes", "ld", ld, "count", len(nodes))
	return nodes, nil
}

// ListDataObjects returns the top-level data objects under the
// specified logical node.
//
// The data objects are discovered by listing all named variables in
// the parent MMS domain, filtering for the specified LN, and extracting
// unique data object names (third $-delimited segment).
//
// Results are sorted alphabetically by name.
func (c *Client) ListDataObjects(ctx context.Context, ld, ln string) ([]DataObject, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}

	if ld == "" {
		return nil, &ReferenceError{Input: ld, Reason: "empty logical device name"}
	}
	if ln == "" {
		return nil, &ReferenceError{Input: ln, Reason: "empty logical node name"}
	}

	items, err := c.cachedItems(ctx, ld)
	if err != nil {
		return nil, fmt.Errorf("iec61850: list data objects in %s/%s: %w", ld, ln, err)
	}

	doNames := mapping.ExtractDataObjects(items, ln)
	objects := make([]DataObject, len(doNames))
	for i, name := range doNames {
		ref := Ref{LD: ld, LN: ln, Path: []string{name}}
		objects[i] = DataObject{Name: name, Reference: ref}
	}

	c.logger.Debug("iec61850: listed data objects", "ld", ld, "ln", ln, "count", len(objects))
	return objects, nil
}

// ListChildren returns the direct browse children (sub-data objects
// and data attributes) under the specified reference.
//
// The ref must include at least LD and LN. When ref has a path (e.g.,
// LD/LN.DO), this returns the direct children of that path. When ref
// has only LD/LN, this is equivalent to [Client.ListDataObjects] but
// returns [BrowseNode] instead of [DataObject].
//
// Because MMS variable names are FC-qualified, the returned children
// are a merged view across all functional constraints. A child name
// like "stVal" may exist under both ST and MX; this method returns
// it once. Use [Client.TreeWithOptions] with IncludeFCs to discover
// which FCs apply, or specify an FC when reading/writing.
//
// Results are sorted alphabetically by name.
func (c *Client) ListChildren(ctx context.Context, ref Ref) ([]BrowseNode, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}

	browseRef := ref
	browseRef.FC = ""
	if err := browseRef.Validate(); err != nil {
		return nil, err
	}
	if browseRef.LN == "" {
		return nil, &ReferenceError{Input: ref.String(), Reason: "logical node required"}
	}
	ref = browseRef

	items, err := c.cachedItems(ctx, ref.LD)
	if err != nil {
		return nil, fmt.Errorf("iec61850: list children for %s: %w", ref.String(), err)
	}

	childNames := mapping.ExtractChildren(items, ref.LN, ref.Path)
	result := make([]BrowseNode, len(childNames))
	for i, name := range childNames {
		childRef, err := ref.Child(name)
		if err != nil {
			return nil, fmt.Errorf("iec61850: list children for %s: invalid child %q: %w", ref.String(), name, err)
		}
		result[i] = BrowseNode{Name: name, Reference: childRef}
	}

	c.logger.Debug("iec61850: listed children", "ref", ref.String(), "count", len(result))
	return result, nil
}

// Tree builds the full IEC 61850 model tree from the server. This
// performs multiple MMS GetNameList calls to discover the complete
// hierarchy: logical devices → logical nodes → data objects/attributes.
//
// The returned root [ModelNode] is a synthetic container (Name="root",
// empty Reference) whose Children are the logical device nodes. It
// does not represent a real IEC 61850 object.
//
// The tree merges structure across functional constraints: the same
// data object path may appear under multiple FCs (e.g., ST and MX),
// and the tree returns it as a single node. Enable
// [TreeOptions.IncludeFCs] to annotate each node with its observed
// FCs. Without FC annotation, a returned node is a browse view, not
// a directly readable reference — callers must choose an FC before
// reading or writing.
//
// The tree includes all data objects and attributes discovered from
// the MMS variable names. For type information, use
// [Client.GetVariableType] on individual nodes.
//
// This operation may be slow on servers with large models.
func (c *Client) Tree(ctx context.Context) (*ModelNode, error) {
	return c.TreeWithOptions(ctx, TreeOptions{})
}

// TreeOptions configures the tree-building behavior for
// [Client.TreeWithOptions].
type TreeOptions struct {
	// LDFilter, when non-empty, restricts the tree to a single
	// logical device matching this name.
	LDFilter string

	// MaxDepth limits how deep the tree is built. Zero means
	// unlimited. Depth counts model-hierarchy levels starting at
	// the logical device level (consistent with [Ref.Depth]):
	//   1 = LD nodes only
	//   2 = LD + LN nodes
	//   3 = LD + LN + first DO level
	//   4+ = deeper data attributes
	//
	// Note: the returned root node is a synthetic container that
	// sits outside the depth model. Its children are the LD nodes
	// (depth 1). MaxDepth=1 therefore returns a root whose
	// children are bare LD nodes with no further expansion.
	MaxDepth int

	// IncludeFCs annotates tree nodes with functional constraint
	// information. When true, each leaf/intermediate node will
	// have its FCs field populated with all FCs observed for that
	// node in the MMS item IDs. The single FC field is set when
	// only one FC applies.
	IncludeFCs bool
}

// TreeWithOptions builds the IEC 61850 model tree with configurable
// filtering and annotation.
//
// Like [Client.Tree], the returned tree merges structure across
// functional constraints. Use [TreeOptions.IncludeFCs] to annotate
// nodes with their observed FCs so that callers can determine which
// FCs are valid for each node before performing read/write operations.
//
// See [Client.Tree] for the simpler zero-options variant.
func (c *Client) TreeWithOptions(ctx context.Context, opts TreeOptions) (*ModelNode, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}

	lds, err := c.cachedLDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("iec61850: tree: %w", err)
	}

	root := &ModelNode{Name: "root"}

	for _, ld := range lds {
		if opts.LDFilter != "" && ld.Name != opts.LDFilter {
			continue
		}

		if opts.MaxDepth > 0 && opts.MaxDepth < 2 {
			root.Children = append(root.Children, &ModelNode{
				Name:      ld.Name,
				Reference: Ref{LD: ld.Name},
			})
			continue
		}

		items, err := c.cachedItems(ctx, ld.Name)
		if err != nil {
			return nil, fmt.Errorf("iec61850: tree: list variables in %q: %w", ld.Name, err)
		}

		ldNode := &ModelNode{
			Name:      ld.Name,
			Reference: Ref{LD: ld.Name},
		}
		ldNode.Children = buildLDTreeWithOptions(ld.Name, items, opts, c.opts.Strictness.RejectUnknownFC, c.logger)
		root.Children = append(root.Children, ldNode)
	}

	if opts.LDFilter != "" && len(root.Children) == 0 {
		return nil, fmt.Errorf("iec61850: tree: %w: no logical device matching filter %q", ErrNotFound, opts.LDFilter)
	}

	sort.Slice(root.Children, func(i, j int) bool {
		return root.Children[i].Name < root.Children[j].Name
	})

	c.logger.Debug("iec61850: built model tree", "devices", len(root.Children))
	return root, nil
}

// FindPaths searches the server model for references matching the
// query. This performs model discovery and then filters the results.
//
// The Pattern field in [FindQuery] supports glob matching via
// [path.Match]. Matching is applied to the full object reference
// string (e.g., "LD/LN.DO.DA"):
//   - '*' matches any characters within a single path component
//     (does not cross '/')
//   - '?' matches a single character
//   - '[' brackets match character classes
//
// For patterns that need to cross component boundaries, consider
// using [MatchRegex] mode instead.
//
// Pattern matching is performed against the object reference without
// the [FC] suffix. If the same object path exists under multiple FCs
// (e.g. ST and MX), it yields one result per FC because the full
// reference string (including FC) is used as the deduplication key.
// To get path-level deduplication, set FindQuery.FC to restrict
// results to a single functional constraint.
//
// The FC field, when non-empty, filters to attributes with that FC.
// Results are deduplicated by full reference string (path + FC).
func (c *Client) FindPaths(ctx context.Context, query FindQuery) ([]Ref, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}

	if query.Pattern == "" {
		return nil, fmt.Errorf("iec61850: find paths: %w: empty pattern", ErrInvalidArgument)
	}

	matcher, err := newMatcher(query.MatchMode, query.Pattern)
	if err != nil {
		return nil, fmt.Errorf("iec61850: find paths: %w", err)
	}

	lds, err := c.cachedLDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("iec61850: find paths: %w", err)
	}

	var results []Ref
	seen := make(map[string]struct{})

	for _, ld := range lds {
		if query.LDFilter != "" && ld.Name != query.LDFilter {
			continue
		}

		refs, err := c.parsedRefsForLD(ctx, ld.Name)
		if err != nil {
			return nil, fmt.Errorf("iec61850: find paths: %w", err)
		}

		for _, ref := range refs {
			if query.FC != "" && ref.FC != query.FC {
				continue
			}

			if query.MaxDepth > 0 && ref.Depth() > query.MaxDepth {
				continue
			}

			refStr := ref.ObjectReference()
			if matcher.match(refStr) {
				key := ref.String()
				if _, dup := seen[key]; !dup {
					seen[key] = struct{}{}
					results = append(results, ref)
				}
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].String() < results[j].String()
	})

	c.logger.Debug("iec61850: find paths", "pattern", query.Pattern, "matches", len(results))
	return results, nil
}

// GetVariableType retrieves the MMS type specification for a variable.
// The ref must be an object reference (LD/LN with path) and must
// include a functional constraint.
func (c *Client) GetVariableType(ctx context.Context, ref Ref) (*mms.TypeSpec, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}

	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if !ref.IsObject() {
		return nil, &ReferenceError{Input: ref.String(), Reason: "object reference required (LD/LN with path)"}
	}
	if ref.FC == "" {
		return nil, &ReferenceError{Input: ref.String(), Reason: "functional constraint required"}
	}

	_, itemID, err := ref.ToMMS()
	if err != nil {
		return nil, err
	}

	name := mms.ObjectName{
		Scope:  mms.ObjectScopeDomain,
		Domain: mms.DomainID(ref.LD),
		ItemID: itemID,
	}

	attrs, err := c.mmsClient.GetVariableAccessAttributes(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("iec61850: get variable type for %s: %w", ref.String(), err)
	}

	return &attrs.TypeSpec, nil
}

// parsedRefsForLD returns pre-parsed Refs for an LD's item list,
// using the cache when available. This avoids re-parsing every MMS
// item ID on every FindPaths call.
func (c *Client) parsedRefsForLD(ctx context.Context, ld string) ([]Ref, error) {
	if c.cache != nil {
		if refs, ok := c.cache.getParsedRefs(ld); ok {
			return refs, nil
		}
	}

	items, err := c.cachedItems(ctx, ld)
	if err != nil {
		return nil, fmt.Errorf("list variables in %q: %w", ld, err)
	}

	refs := make([]Ref, 0, len(items))
	for _, item := range items {
		ref, parseErr := RefFromMMS(mms.DomainID(ld), mms.ItemID(item))
		if parseErr != nil {
			if c.opts.Strictness.RejectUnknownFC {
				return nil, parseErr
			}
			c.logger.Debug("iec61850: skipping malformed MMS item during find",
				"ld", ld, "item", item, "error", parseErr)
			continue
		}
		refs = append(refs, ref)
	}

	if c.cache != nil {
		c.cache.setParsedRefs(ld, refs)
	}

	return refs, nil
}

// buildLDTreeWithOptions constructs the tree of LN → DO → DA nodes
// from a flat list of MMS item IDs, applying tree options.
func buildLDTreeWithOptions(ld string, items []string, opts TreeOptions, rejectUnknownFC bool, logger *slog.Logger) []*ModelNode {
	lnNames := mapping.ExtractLogicalNodes(items)
	lnNodes := make([]*ModelNode, 0, len(lnNames))

	for _, lnName := range lnNames {
		lnNode := &ModelNode{
			Name:      lnName,
			Reference: Ref{LD: ld, LN: lnName},
		}

		if opts.IncludeFCs {
			lnNode.FCs = convertFCs(mapping.ExtractFCsForLN(items, lnName), rejectUnknownFC, logger)
			if len(lnNode.FCs) == 1 {
				lnNode.FC = lnNode.FCs[0]
			}
		}

		if opts.MaxDepth > 0 && opts.MaxDepth <= 2 {
			lnNodes = append(lnNodes, lnNode)
			continue
		}

		doNames := mapping.ExtractDataObjects(items, lnName)
		for _, doName := range doNames {
			doRef := Ref{LD: ld, LN: lnName, Path: []string{doName}}
			doNode := &ModelNode{
				Name:      doName,
				Reference: doRef,
			}

			if opts.IncludeFCs {
				doNode.FCs = convertFCs(mapping.ExtractFCsForPath(items, lnName, []string{doName}), rejectUnknownFC, logger)
				if len(doNode.FCs) == 1 {
					doNode.FC = doNode.FCs[0]
				}
			}

			if opts.MaxDepth <= 0 || opts.MaxDepth > 3 {
				buildSubTreeWithOptions(doNode, ld, lnName, items, []string{doName}, opts, rejectUnknownFC, logger)
			}
			lnNode.Children = append(lnNode.Children, doNode)
		}

		lnNodes = append(lnNodes, lnNode)
	}

	return lnNodes
}

// buildSubTreeWithOptions recursively expands child nodes. Note: each
// level re-scans the flat item list via ExtractChildren and
// ExtractFCsForPath, which is O(n) per level. For very large models
// (thousands of items) a trie-based index would reduce tree build
// cost. For typical substation sizes this is adequate.
func buildSubTreeWithOptions(node *ModelNode, ld, ln string, items []string, basePath []string, opts TreeOptions, rejectUnknownFC bool, logger *slog.Logger) {
	currentDepth := 2 + len(basePath) // LD(1) + LN(2) + path components
	if opts.MaxDepth > 0 && currentDepth >= opts.MaxDepth {
		return
	}

	childNames := mapping.ExtractChildren(items, ln, basePath)
	for _, name := range childNames {
		childPath := make([]string, len(basePath)+1)
		copy(childPath, basePath)
		childPath[len(basePath)] = name

		childRef := Ref{LD: ld, LN: ln, Path: childPath}
		child := &ModelNode{
			Name:      name,
			Reference: childRef,
		}

		if opts.IncludeFCs {
			child.FCs = convertFCs(mapping.ExtractFCsForPath(items, ln, childPath), rejectUnknownFC, logger)
			if len(child.FCs) == 1 {
				child.FC = child.FCs[0]
			}
		}

		buildSubTreeWithOptions(child, ld, ln, items, childPath, opts, rejectUnknownFC, logger)
		node.Children = append(node.Children, child)
	}
}

// convertFCs converts FC strings to FunctionalConstraint values. When
// rejectUnknown is true, non-standard FCs are filtered out and logged
// at debug level; otherwise all strings are accepted as-is.
func convertFCs(strs []string, rejectUnknown bool, logger *slog.Logger) []FunctionalConstraint {
	if !rejectUnknown {
		fcs := make([]FunctionalConstraint, len(strs))
		for i, s := range strs {
			fcs[i] = FunctionalConstraint(s)
		}
		return fcs
	}
	fcs := make([]FunctionalConstraint, 0, len(strs))
	for _, s := range strs {
		fc, err := ParseFC(s)
		if err == nil {
			fcs = append(fcs, fc)
		} else if logger != nil {
			logger.Debug("iec61850: dropping unknown FC during tree build", "fc", s)
		}
	}
	return fcs
}

// matcher abstracts glob vs regex pattern matching.
type matcher struct {
	mode  MatchMode
	glob  string
	regex *regexp.Regexp
}

func newMatcher(mode MatchMode, pattern string) (*matcher, error) {
	switch mode {
	case MatchGlob:
		if _, err := path.Match(pattern, ""); err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
		}
		return &matcher{mode: MatchGlob, glob: pattern}, nil
	case MatchRegex:
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern %q: %w", pattern, err)
		}
		return &matcher{mode: MatchRegex, regex: re}, nil
	default:
		return nil, fmt.Errorf("unknown match mode %d", mode)
	}
}

func (m *matcher) match(s string) bool {
	switch m.mode {
	case MatchGlob:
		ok, _ := path.Match(m.glob, s)
		return ok
	case MatchRegex:
		return m.regex.MatchString(s)
	default:
		return false
	}
}
