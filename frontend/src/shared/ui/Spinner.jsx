export function Spinner({ label }) {
  return (
    <div className="row">
      <span className="spinner" />
      {label ? <span className="muted">{label}</span> : null}
    </div>
  );
}
