package Cx

import data.generic.common as common_lib
import data.generic.k8s as k8sLib

types := {"initContainers", "containers"}

CxPolicy[result] {
  document := input.document[i]
  metadata := document.metadata

  specInfo := k8sLib.getSpecInfo(document)
  spec := specInfo.spec

  podSC := object.get(spec, "securityContext", {})
  podUID := object.get(podSC, "runAsUser", null)

  container := specInfo.spec[types[t]][c]
  cSC := object.get(container, "securityContext", {})
  cUID := object.get(cSC, "runAsUser", podUID)

  cUID == null

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.%s.securityContext.runAsUser", [metadata.name, specInfo.path]),
    "issueType": "MissingAttribute",
    "keyExpectedValue": "runAsUser should be set to a non-zero UID",
    "keyActualValue": "runAsUser is undefined",
    "searchLine": common_lib.build_search_line(split(specInfo.path, "."), ["securityContext", "runAsUser"]),
  }
}

CxPolicy[result] {
  document := input.document[i]
  metadata := document.metadata

  specInfo := k8sLib.getSpecInfo(document)
  spec := specInfo.spec

  podSC := object.get(spec, "securityContext", {})
  podUID := object.get(podSC, "runAsUser", null)

  container := specInfo.spec[types[t]][c]
  cSC := object.get(container, "securityContext", {})
  cUID := object.get(cSC, "runAsUser", podUID)

  cUID != null
  to_number(cUID) == 0

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.%s.securityContext.runAsUser", [metadata.name, specInfo.path]),
    "issueType": "IncorrectValue",
    "keyExpectedValue": "runAsUser should be non-zero",
    "keyActualValue": "runAsUser is 0",
    "searchLine": common_lib.build_search_line(split(specInfo.path, "."), ["securityContext", "runAsUser"]),
  }
}
