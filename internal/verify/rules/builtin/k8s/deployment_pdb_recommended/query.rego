package Cx

import data.generic.common as common_lib

is_subset(a, b) {
  keys := object.keys(a)
  count(keys) > 0
  not has_mismatch(keys, a, b)
}

has_mismatch(keys, a, b) {
  k := keys[_]
  object.get(b, k, "__missing") != object.get(a, k, "__missing")
}

has_matching_pdb(ns, sel) {
  some j
  pdb := input.document[j]
  pdb.kind == "PodDisruptionBudget"
  pdb.metadata.namespace == ns
  spec := object.get(pdb, "spec", {})
  s := object.get(object.get(spec, "selector", {}), "matchLabels", {})
  is_subset(s, sel)
}

CxPolicy[result] {
  document := input.document[i]
  document.kind == "Deployment"
  metadata := document.metadata

  spec := object.get(document, "spec", {})
  replicas := object.get(spec, "replicas", 1)
  to_number(replicas) >= 3

  ns := object.get(metadata, "namespace", "default")
  sel := object.get(object.get(spec, "selector", {}), "matchLabels", {})

  not has_matching_pdb(ns, sel)

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.spec.selector.matchLabels", [metadata.name]),
    "issueType": "MissingAttribute",
    "keyExpectedValue": sprintf("metadata.name={{%s}} should have a matching PodDisruptionBudget", [metadata.name]),
    "keyActualValue": sprintf("metadata.name={{%s}} has 3+ replicas without a matching PDB in this render", [metadata.name]),
    "searchLine": common_lib.build_search_line([], ["spec", "selector", "matchLabels"]),
  }
}
