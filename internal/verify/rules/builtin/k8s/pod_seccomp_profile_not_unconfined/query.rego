package Cx

import data.generic.common as common_lib
import data.generic.k8s as k8sLib

types := {"initContainers", "containers"}

is_unconfined(t) {
  lower(t) == "unconfined"
}

# Container-level seccompProfile.type must not be Unconfined.
CxPolicy[result] {
  document := input.document[i]
  metadata := document.metadata

  specInfo := k8sLib.getSpecInfo(document)
  container := specInfo.spec[types[t]][c]

  sc := object.get(container, "securityContext", {})
  sp := object.get(sc, "seccompProfile", {})
  typ := object.get(sp, "type", "")

  is_unconfined(typ)

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.%s.%s.name={{%s}}.securityContext.seccompProfile.type", [metadata.name, specInfo.path, types[t], container.name]),
    "issueType": "IncorrectValue",
    "keyExpectedValue": "seccompProfile.type should not be Unconfined",
    "keyActualValue": "seccompProfile.type is Unconfined",
    "searchLine": common_lib.build_search_line(split(specInfo.path, "."), [types[t], c, "securityContext", "seccompProfile", "type"])
  }
}

# Pod-level seccompProfile.type must not be Unconfined.
CxPolicy[result] {
  document := input.document[i]
  metadata := document.metadata

  specInfo := k8sLib.getSpecInfo(document)
  spec := specInfo.spec

  psc := object.get(spec, "securityContext", {})
  psp := object.get(psc, "seccompProfile", {})
  ptyp := object.get(psp, "type", "")

  is_unconfined(ptyp)

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.%s.securityContext.seccompProfile.type", [metadata.name, specInfo.path]),
    "issueType": "IncorrectValue",
    "keyExpectedValue": "seccompProfile.type should not be Unconfined",
    "keyActualValue": "seccompProfile.type is Unconfined",
    "searchLine": common_lib.build_search_line(split(specInfo.path, "."), ["securityContext", "seccompProfile", "type"])
  }
}
