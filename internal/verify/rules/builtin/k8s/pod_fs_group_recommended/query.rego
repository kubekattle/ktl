package Cx

import data.generic.common as common_lib
import data.generic.k8s as k8sLib

CxPolicy[result] {
  document := input.document[i]
  metadata := document.metadata

  specInfo := k8sLib.getSpecInfo(document)
  spec := specInfo.spec

  podSC := object.get(spec, "securityContext", {})
  fsg := object.get(podSC, "fsGroup", null)

  fsg == null

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.%s.securityContext.fsGroup", [metadata.name, specInfo.path]),
    "issueType": "MissingAttribute",
    "keyExpectedValue": "fsGroup should be set to a non-zero GID",
    "keyActualValue": "fsGroup is undefined",
    "searchLine": common_lib.build_search_line(split(specInfo.path, "."), ["securityContext", "fsGroup"]),
  }
}

CxPolicy[result] {
  document := input.document[i]
  metadata := document.metadata

  specInfo := k8sLib.getSpecInfo(document)
  spec := specInfo.spec

  podSC := object.get(spec, "securityContext", {})
  fsg := object.get(podSC, "fsGroup", null)

  fsg != null
  to_number(fsg) == 0

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.%s.securityContext.fsGroup", [metadata.name, specInfo.path]),
    "issueType": "IncorrectValue",
    "keyExpectedValue": "fsGroup should be non-zero",
    "keyActualValue": "fsGroup is 0",
    "searchLine": common_lib.build_search_line(split(specInfo.path, "."), ["securityContext", "fsGroup"]),
  }
}
