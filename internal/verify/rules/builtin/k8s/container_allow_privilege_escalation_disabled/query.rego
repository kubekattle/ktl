package Cx

import data.generic.common as common_lib
import data.generic.k8s as k8sLib

types := {"initContainers", "containers"}

CxPolicy[result] {
  document := input.document[i]
  metadata := document.metadata

  specInfo := k8sLib.getSpecInfo(document)
  container := specInfo.spec[types[t]][c]

  security := object.get(container, "securityContext", {})
  ape := object.get(security, "allowPrivilegeEscalation", true)
  ape != false

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.%s.%s.name={{%s}}.securityContext.allowPrivilegeEscalation", [metadata.name, specInfo.path, types[t], container.name]),
    "issueType": "IncorrectValue",
    "keyExpectedValue": "allowPrivilegeEscalation should be false",
    "keyActualValue": sprintf("allowPrivilegeEscalation is %v", [ape]),
    "searchLine": common_lib.build_search_line(split(specInfo.path, "."), [types[t], c, "securityContext", "allowPrivilegeEscalation"]),
  }
}
