import useSWR from "swr";
import type { ConfigField } from "../../typings/Destination";
import { CopyButton } from "../CopyButton/CopyButton";
import "./WorkloadIdentityInfo.scss";

const AUTH_METHOD = "auth_method";
const WORKLOAD_IDENTITY = "workload_identity";

/**
 * Whether the field is only shown for workload identity authentication.
 */
export const isWorkloadIdentityField = (field: ConfigField) =>
  field.visible_when?.key === AUTH_METHOD &&
  field.visible_when.values.includes(WORKLOAD_IDENTITY);

interface WorkloadIdentity {
  issuer: string;
  subject: string;
}

const WorkloadIdentityInfo = () => {
  const { data } = useSWR<WorkloadIdentity>("workload-identity");

  if (!data) {
    return null;
  }

  return (
    <div className="workload-identity-info">
      <p className="body-m">
        Allow this issuer and subject in your workload identity provider. Steps
        are in the Configuration Guide.
      </p>
      <dl>
        <dt className="body-m">Issuer</dt>
        <dd className="mono-s">
          <span>{data.issuer}</span> <CopyButton value={data.issuer} />
        </dd>
        <dt className="body-m">Subject</dt>
        <dd className="mono-s">
          <span>{data.subject}</span> <CopyButton value={data.subject} />
        </dd>
      </dl>
    </div>
  );
};

export default WorkloadIdentityInfo;
