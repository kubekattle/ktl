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
	run_as_non_root := object.get(security, "runAsNonRoot", false)
	run_as_non_root != true

	result := {
		"documentId": document.id,
		"resourceType": document.kind,
		"resourceName": metadata.name,
		"searchKey": sprintf("metadata.name={{%s}}.%s.%s.name={{%s}}.securityContext.runAsNonRoot", [metadata.name, specInfo.path, types[t], container.name]),
		"issueType": "IncorrectValue",
		"keyExpectedValue": sprintf("metadata.name={{%s}}.%s.%s.name={{%s}}.securityContext.runAsNonRoot should be true", [metadata.name, specInfo.path, types[t], container.name]),
		"keyActualValue": sprintf("metadata.name={{%s}}.%s.%s.name={{%s}}.securityContext.runAsNonRoot is %v", [metadata.name, specInfo.path, types[t], container.name, run_as_non_root]),
		"searchLine": common_lib.build_search_line(split(specInfo.path, "."), [types[t], c, "securityContext", "runAsNonRoot"]),
	}
}
