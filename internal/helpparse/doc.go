// Package helpparse fills missing overlay completion descriptions from --help
// text. It follows mandible's model (https://github.com/AS-FOSS/mandible):
// identify the CLI framework that generated the text, apply that grammar, and
// never special-case a tool name.
//
// Parent-page parse fills sibling one-liners when the list includes them.
// When a parent page is only a bare name list, FillNodes lazily probes each
// visible row's own help and takes a NAME/DESCRIPTION one-liner from that page.
//
// Signature strings and the identify-then-parse approach are adapted from
// mandible-extract (MIT / Apache-2.0), Copyright AS-FOSS contributors.
package helpparse
