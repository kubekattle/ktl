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
  caps := object.get(security, "capabilities", {})
  drop := object.get(caps, "drop", [])

  not common_lib.inArray(drop, "ALL")

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.%s.%s.name={{%s}}.securityContext.capabilities.drop", [metadata.name, specInfo.path, types[t], container.name]),
    "issueType": "IncorrectValue",
    "keyExpectedValue": "capabilities.drop should include ALL",
    "keyActualValue": "capabilities.drop does not include ALL",
    "searchLine": common_lib.build_search_line(split(specInfo.path, "."), [types[t], c, "securityContext", "capabilities", "drop"]),
  }
}
