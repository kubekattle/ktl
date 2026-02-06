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
  roleRef := object.get(document, "roleRef", {})
  lower(object.get(roleRef, "kind", "")) == "clusterrole"
  object.get(roleRef, "name", "") == "cluster-admin"

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.roleRef.name", [metadata.name]),
    "issueType": "IncorrectValue",
    "keyExpectedValue": sprintf("metadata.name={{%s}}.roleRef.name should not be cluster-admin", [metadata.name]),
    "keyActualValue": sprintf("metadata.name={{%s}}.roleRef.name is cluster-admin", [metadata.name]),
    "searchLine": common_lib.build_search_line([], ["roleRef", "name"]),
  }
}
