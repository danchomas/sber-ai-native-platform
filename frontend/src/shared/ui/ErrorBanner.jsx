export function ErrorBanner({ error, onRetry }) {
  if (!error) return null;
  return (
    <div className="notice error">
      <span>✕</span>
      <span>{error.message || String(error)}</span>
      {onRetry ? (
        <button className="icon-btn" style={{ marginLeft: "auto" }} onClick={onRetry}>
          Повторить
        </button>
      ) : null}
    </div>
  );
}
