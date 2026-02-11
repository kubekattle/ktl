package Cx

import data.generic.common as common_lib

has_hpa(ns, name) {
  some j
  h := input.document[j]
  h.kind == "HorizontalPodAutoscaler"
  h.metadata.namespace == ns
  ref := object.get(object.get(h, "spec", {}), "scaleTargetRef", {})
  lower(object.get(ref, "kind", "")) == "deployment"
  object.get(ref, "name", "") == name
}

CxPolicy[result] {
  document := input.document[i]
  document.kind == "Deployment"
  metadata := document.metadata

  spec := object.get(document, "spec", {})
  replicas := object.get(spec, "replicas", 1)
  to_number(replicas) >= 3

  ns := object.get(metadata, "namespace", "default")
  name := metadata.name
  not has_hpa(ns, name)

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.spec.replicas", [metadata.name]),
    "issueType": "MissingAttribute",
    "keyExpectedValue": sprintf("metadata.name={{%s}} should have a HorizontalPodAutoscaler", [metadata.name]),
    "keyActualValue": sprintf("metadata.name={{%s}} has 3+ replicas without an HPA in this render", [metadata.name]),
    "searchLine": common_lib.build_search_line([], ["spec", "replicas"]),
  }
}
