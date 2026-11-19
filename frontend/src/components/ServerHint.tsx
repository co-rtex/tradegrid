/** What to do when the backend does not answer. Shared by every live view. */
export default function ServerHint() {
  return (
    <p>
      Check that the backend is running (by default on 127.0.0.1:8080), then press Refresh. If it restarted, every
      demo account is back to its starting balance.
    </p>
  )
}
