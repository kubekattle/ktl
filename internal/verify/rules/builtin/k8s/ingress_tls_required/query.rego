package Cx

import data.generic.common as common_lib

CxPolicy[result] {
  document := input.document[i]
  document.kind == "Ingress"
  metadata := document.metadata

  spec := object.get(document, "spec", {})
  tls := object.get(spec, "tls", [])
  count(tls) == 0

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.spec.tls", [metadata.name]),
    "issueType": "MissingAttribute",
    "keyExpectedValue": sprintf("metadata.name={{%s}}.spec.tls should be defined", [metadata.name]),
    "keyActualValue": sprintf("metadata.name={{%s}}.spec.tls is undefined", [metadata.name]),
    "searchLine": common_lib.build_search_line([], ["spec", "tls"]),
  }
}
