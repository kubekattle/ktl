package Cx

import data.generic.common as common_lib

CxPolicy[result] {
  document := input.document[i]
  document.kind == "ClusterRole"

  metadata := document.metadata
  rules := object.get(document, "rules", [])
  some r
  rule := rules[r]

  verbs := object.get(rule, "verbs", [])
  resources := object.get(rule, "resources", [])

  common_lib.inArray(verbs, "*")
  result := mk_result(document, metadata, r, "verbs")
}

CxPolicy[result] {
  document := input.document[i]
  document.kind == "ClusterRole"

  metadata := document.metadata
  rules := object.get(document, "rules", [])
  some r
  rule := rules[r]

  verbs := object.get(rule, "verbs", [])
  resources := object.get(rule, "resources", [])

  not common_lib.inArray(verbs, "*")
  common_lib.inArray(resources, "*")
  result := mk_result(document, metadata, r, "resources")
}

mk_result(document, metadata, idx, field) = result {
  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.rules", [metadata.name]),
    "issueType": "IncorrectValue",
    "keyExpectedValue": sprintf("metadata.name={{%s}}.rules[%d].%s should not include '*'", [metadata.name, idx, field]),
    "keyActualValue": sprintf("metadata.name={{%s}}.rules[%d].%s includes '*'", [metadata.name, idx, field]),
    "searchLine": common_lib.build_search_line([], ["rules", idx, field]),
  }
}
