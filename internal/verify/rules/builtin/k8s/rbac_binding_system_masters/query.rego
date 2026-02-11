package Cx

import data.generic.common as common_lib

binding_kind(kind) {
  kind == "RoleBinding"
}
binding_kind(kind) {
  kind == "ClusterRoleBinding"
}

CxPolicy[result] {
  document := input.document[i]
  binding_kind(document.kind)

  metadata := document.metadata
  subjects := object.get(document, "subjects", [])

  some s
  subj := subjects[s]
  lower(object.get(subj, "kind", "")) == "group"
  object.get(subj, "name", "") == "system:masters"

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.subjects", [metadata.name]),
    "issueType": "IncorrectValue",
    "keyExpectedValue": sprintf("metadata.name={{%s}}.subjects should not include system:masters", [metadata.name]),
    "keyActualValue": sprintf("metadata.name={{%s}}.subjects includes system:masters", [metadata.name]),
    "searchLine": common_lib.build_search_line([], ["subjects", s, "name"]),
  }
}
