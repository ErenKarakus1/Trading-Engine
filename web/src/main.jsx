import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

const DEFAULT_SYMBOL = "BTC-USD";
const SYMBOLS = ["BTC-USD", "ETH-USD"];

function marketSymbolFor(symbol) {
  const [base, quote] = symbol.split("-");
  return base && quote === "USD" ? `${base}-USDT` : symbol;
}

function valueOf(source, ...keys) {
  if (!source) return undefined;
  for (const key of keys) {
    if (source[key] !== undefined && source[key] !== null) return source[key];
  }
  return undefined;
}

function numberText(value) {
  if (value === undefined || value === null || value === "") return "-";
  const number = Number(value);
  if (!Number.isFinite(number)) return String(value);
  return new Intl.NumberFormat("en-US").format(number);
}

function scaledText(value, scale) {
  if (value === undefined || value === null || value === "") return "-";
  const number = Number(value);
  if (!Number.isFinite(number)) return String(value);
  return new Intl.NumberFormat("en-US", { maximumFractionDigits: 8 }).format(number / scale);
}

function diff(bid, ask) {
  if (bid === undefined || ask === undefined) return null;
  return Number(ask) - Number(bid);
}

function App() {
  const [symbol, setSymbol] = useState(DEFAULT_SYMBOL);
  const [symbolInput, setSymbolInput] = useState(DEFAULT_SYMBOL);
  const [bookSource, setBookSource] = useState("internal");
  const [connection, setConnection] = useState("connecting");
  const [book, setBook] = useState({ symbol: DEFAULT_SYMBOL });
  const [marketBook, setMarketBook] = useState({ symbol: marketSymbolFor(DEFAULT_SYMBOL) });
  const [trades, setTrades] = useState([]);
  const [events, setEvents] = useState([]);
  const [error, setError] = useState("");
  const [ticket, setTicket] = useState({
    accountId: "demo",
    side: "buy",
    type: "limit",
    price: "101",
    quantity: "1",
  });
  const [ticketStatus, setTicketStatus] = useState("Ready");
  const [submitting, setSubmitting] = useState(false);
  const internalSocket = useRef(null);
  const marketSocket = useRef(null);
  const marketSymbol = useMemo(() => marketSymbolFor(symbol), [symbol]);

  const refresh = useCallback(async (activeSymbol) => {
    const encoded = encodeURIComponent(activeSymbol);
    const [bookResponse, tradesResponse] = await Promise.all([
      fetch(`/orderbook/${encoded}`),
      fetch(`/trades/${encoded}`),
    ]);
    if (!bookResponse.ok) throw new Error(`orderbook ${bookResponse.status}`);
    if (!tradesResponse.ok) throw new Error(`trades ${tradesResponse.status}`);
    setBook(await bookResponse.json());
    setTrades(await tradesResponse.json());
    setError("");
  }, []);

  useEffect(() => {
    let active = true;
    setConnection("connecting");
    refresh(symbol).catch((err) => active && setError(err.message));

    if (internalSocket.current) internalSocket.current.close();
    const protocol = location.protocol === "https:" ? "wss" : "ws";
    const socket = new WebSocket(`${protocol}://${location.host}/ws/orderbook/${encodeURIComponent(symbol)}`);
    internalSocket.current = socket;
    socket.onopen = () => active && setConnection("live");
    socket.onerror = () => active && setError("internal websocket unavailable");
    socket.onclose = () => active && setConnection("offline");
    socket.onmessage = (event) => {
      const message = JSON.parse(event.data);
      if (message.type === "snapshot" && message.book) setBook(message.book);
      if (message.type === "events") {
        setEvents((current) => [...message.events, ...current].slice(0, 120));
        refresh(symbol).catch((err) => active && setError(err.message));
      }
    };
    return () => {
      active = false;
      socket.close();
    };
  }, [refresh, symbol]);

  useEffect(() => {
    let active = true;
    setMarketBook({ symbol: marketSymbol });
    fetch(`/marketdata/${encodeURIComponent(marketSymbol)}`)
      .then((response) => (response.ok ? response.json() : null))
      .then((snapshot) => active && snapshot && setMarketBook(snapshot))
      .catch(() => {});

    if (marketSocket.current) marketSocket.current.close();
    const protocol = location.protocol === "https:" ? "wss" : "ws";
    const socket = new WebSocket(`${protocol}://${location.host}/ws/marketdata/${encodeURIComponent(marketSymbol)}`);
    marketSocket.current = socket;
    socket.onmessage = (event) => {
      const message = JSON.parse(event.data);
      if (message.type === "snapshot" && message.snapshot) setMarketBook(message.snapshot);
    };
    return () => {
      active = false;
      socket.close();
    };
  }, [marketSymbol]);

  const setActiveSymbol = useCallback((next) => {
    setSymbolInput(next);
    setSymbol(next);
  }, []);

  const loadTypedSymbol = useCallback(() => {
    const next = symbolInput.trim().toUpperCase();
    if (next) setActiveSymbol(next);
  }, [setActiveSymbol, symbolInput]);

  const updateTicket = useCallback((key, value) => {
    setTicket((current) => ({ ...current, [key]: value }));
  }, []);

  const submitOrder = useCallback(async (event) => {
    event.preventDefault();
    setSubmitting(true);
    setTicketStatus("Sending");
    const order = {
      account_id: ticket.accountId.trim(),
      id: `ui-${Date.now()}`,
      symbol,
      side: ticket.side,
      type: ticket.type,
      quantity: Number(ticket.quantity),
    };
    if (ticket.type === "limit") order.price = Number(ticket.price);

    try {
      const response = await fetch("/orders", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(order),
      });
      const body = await response.json();
      if (!response.ok) throw new Error(body.error || `order ${response.status}`);
      setTicketStatus(`Accepted ${order.id}`);
      await refresh(symbol);
    } catch (err) {
      setTicketStatus(err.message);
    } finally {
      setSubmitting(false);
    }
  }, [refresh, symbol, ticket]);

  const internalBid = valueOf(book, "best_bid", "BestBid");
  const internalAsk = valueOf(book, "best_ask", "BestAsk");
  const internalBidPrice = valueOf(internalBid, "price", "Price");
  const internalAskPrice = valueOf(internalAsk, "price", "Price");
  const marketBids = marketBook.bids || marketBook.Bids || [];
  const marketAsks = marketBook.asks || marketBook.Asks || [];
  const marketBidRaw = valueOf(marketBids[0], "price", "Price");
  const marketAskRaw = valueOf(marketAsks[0], "price", "Price");
  const marketBidPrice = marketBidRaw === undefined ? undefined : Number(marketBidRaw) / 100;
  const marketAskPrice = marketAskRaw === undefined ? undefined : Number(marketAskRaw) / 100;
  const selectedBookTitle = bookSource === "internal" ? "Internal Engine Book" : "Binance Reference Book";
  const selectedBid = bookSource === "internal" ? internalBidPrice : marketBidPrice;
  const selectedAsk = bookSource === "internal" ? internalAskPrice : marketAskPrice;
  const selectedSpread = diff(selectedBid, selectedAsk);
  const selectedSourceLabel = bookSource === "internal" ? symbol : marketSymbol;
  const volume = trades.reduce((sum, trade) => sum + Number(valueOf(trade, "quantity", "Quantity") || 0), 0);

  return (
    <div className="app-shell">
      <aside className="market-rail">
        <div className="rail-brand">
          <span>TE</span>
          <strong>Trading Engine</strong>
        </div>
        <div className="rail-section">
          <p>Markets</p>
          {SYMBOLS.map((item) => (
            <button
              className={item === symbol ? "market-button active" : "market-button"}
              key={item}
              type="button"
              onClick={() => setActiveSymbol(item)}
            >
              <span>{item}</span>
              <small>{marketSymbolFor(item)}</small>
            </button>
          ))}
        </div>
      </aside>

      <main className="desk">
        <header className="desk-header">
          <div>
            <span className="label">Active symbol</span>
            <h1>{symbol}</h1>
          </div>
          <div className="symbol-loader">
            <input
              value={symbolInput}
              spellCheck="false"
              onChange={(event) => setSymbolInput(event.target.value)}
              onKeyDown={(event) => event.key === "Enter" && loadTypedSymbol()}
            />
            <button type="button" onClick={loadTypedSymbol}>Load</button>
          </div>
          <div className={`connection ${connection}`}>{connection}</div>
        </header>

        {error ? <div className="system-alert">{error}</div> : null}

        <section className="summary-strip">
          <Stat label={`${bookSource} symbol`} value={selectedSourceLabel} />
          <Stat label="Best Bid" value={selectedBid} tone="buy" />
          <Stat label="Best Ask" value={selectedAsk} tone="sell" />
          <Stat label="Spread" value={selectedSpread} />
          {bookSource === "internal" ? <Stat label="Trades" value={trades.length} /> : <Stat label="Depth" value={marketBids.length + marketAsks.length} />}
          {bookSource === "internal" ? <Stat label="Volume" value={volume} /> : <Stat label="Feed" value="Binance" />}
        </section>

        <section className="desk-body">
          <section className="primary-column">
            <div className="module book-module">
              <div className="module-header">
                <div>
                  <span className="label">Book source</span>
                  <h2>{selectedBookTitle}</h2>
                </div>
                <Segmented value={bookSource} onChange={setBookSource} />
              </div>
              {bookSource === "internal" ? (
                <InternalBook bid={internalBid} ask={internalAsk} />
              ) : (
                <ExternalBook bids={marketBids} asks={marketAsks} />
              )}
            </div>

            {bookSource === "internal" ? (
              <div className="activity-grid">
                <div className="module">
                  <ModuleHeader title="Trade Tape" meta={`${numberText(trades.length)} trades`} />
                  <TradesTable trades={trades} />
                </div>
                <div className="module">
                  <ModuleHeader title="Event Log" meta={`${numberText(events.length)} events`} />
                  <EventLog events={events} />
                </div>
              </div>
            ) : (
              <div className="module">
                <ModuleHeader title="Binance Depth" meta={marketSymbol} />
                <DepthPreview bids={marketBids} asks={marketAsks} />
              </div>
            )}
          </section>

          {bookSource === "internal" ? (
            <aside className="right-rail">
              <div className="module">
                <ModuleHeader title="Order Ticket" meta={symbol} />
                <OrderTicket
                  ticket={ticket}
                  submitting={submitting}
                  status={ticketStatus}
                  onChange={updateTicket}
                  onSubmit={submitOrder}
                />
              </div>
              <div className="module">
                <ModuleHeader title="Session" meta="current" />
                <div className="session-list">
                  <Stat label="Trades" value={trades.length} />
                  <Stat label="Volume" value={volume} />
                  <Stat label="Events" value={events.length} />
                </div>
              </div>
            </aside>
          ) : null}
        </section>
      </main>
    </div>
  );
}

function Segmented({ value, onChange }) {
  return (
    <div className="segmented">
      <button className={value === "internal" ? "active" : ""} type="button" onClick={() => onChange("internal")}>Internal</button>
      <button className={value === "binance" ? "active" : ""} type="button" onClick={() => onChange("binance")}>Binance</button>
    </div>
  );
}

function ModuleHeader({ title, meta }) {
  return (
    <div className="module-header compact">
      <h2>{title}</h2>
      <span>{meta}</span>
    </div>
  );
}

function Stat({ label, value, tone }) {
  return (
    <div className="stat">
      <span>{label}</span>
      <strong className={tone || ""}>{numberText(value)}</strong>
    </div>
  );
}

function DepthPreview({ bids, asks }) {
  return (
    <table className="data-table">
      <thead>
        <tr>
          <th>Bid Price</th>
          <th>Bid Qty</th>
          <th>Ask Price</th>
          <th>Ask Qty</th>
        </tr>
      </thead>
      <tbody>
        {Array.from({ length: Math.max(bids.length, asks.length, 1) }).slice(0, 10).map((_, index) => {
          const bid = bids[index];
          const ask = asks[index];
          return (
            <tr key={index}>
              <td className="buy">{scaledText(valueOf(bid, "price", "Price"), 100)}</td>
              <td>{scaledText(valueOf(bid, "quantity", "Quantity"), 100_000_000)}</td>
              <td className="sell">{scaledText(valueOf(ask, "price", "Price"), 100)}</td>
              <td>{scaledText(valueOf(ask, "quantity", "Quantity"), 100_000_000)}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

function OrderTicket({ ticket, submitting, status, onChange, onSubmit }) {
  return (
    <form className="ticket" onSubmit={onSubmit}>
      <div className="side-picker">
        <button className={ticket.side === "buy" ? "active buy-bg" : ""} type="button" onClick={() => onChange("side", "buy")}>Buy</button>
        <button className={ticket.side === "sell" ? "active sell-bg" : ""} type="button" onClick={() => onChange("side", "sell")}>Sell</button>
      </div>
      <Field label="Account">
        <input value={ticket.accountId} autoComplete="off" onChange={(event) => onChange("accountId", event.target.value)} />
      </Field>
      <Field label="Type">
        <select value={ticket.type} onChange={(event) => onChange("type", event.target.value)}>
          <option value="limit">Limit</option>
          <option value="market">Market</option>
        </select>
      </Field>
      <div className="ticket-pair">
        <Field label="Price">
          <input type="number" min="1" step="1" value={ticket.price} disabled={ticket.type === "market"} onChange={(event) => onChange("price", event.target.value)} />
        </Field>
        <Field label="Quantity">
          <input type="number" min="1" step="1" value={ticket.quantity} onChange={(event) => onChange("quantity", event.target.value)} />
        </Field>
      </div>
      <button className={`submit ${ticket.side === "buy" ? "buy-bg" : "sell-bg"}`} type="submit" disabled={submitting}>
        {submitting ? "Sending" : `${ticket.side === "buy" ? "Buy" : "Sell"} ${ticket.type}`}
      </button>
      <div className="ticket-note">{status}</div>
    </form>
  );
}

function Field({ label, children }) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
    </label>
  );
}

function InternalBook({ bid, ask }) {
  return (
    <table className="data-table">
      <thead>
        <tr>
          <th>Side</th>
          <th>Price</th>
          <th>Quantity</th>
          <th>Orders</th>
        </tr>
      </thead>
      <tbody>
        {[
          ["Bid", bid, "buy"],
          ["Ask", ask, "sell"],
        ].map(([label, level, tone]) => (
          <tr key={label}>
            <td className={tone}>{label}</td>
            <td>{numberText(valueOf(level, "price", "Price"))}</td>
            <td>{numberText(valueOf(level, "quantity", "Quantity"))}</td>
            <td>{numberText(valueOf(level, "orders", "Orders"))}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function ExternalBook({ bids, asks }) {
  return (
    <table className="data-table">
      <thead>
        <tr>
          <th>Side</th>
          <th>Price</th>
          <th>Quantity</th>
        </tr>
      </thead>
      <tbody>
        {[
          ["Bid", bids[0], "buy"],
          ["Ask", asks[0], "sell"],
        ].map(([label, level, tone]) => (
          <tr key={label}>
            <td className={tone}>{label}</td>
            <td>{scaledText(valueOf(level, "price", "Price"), 100)}</td>
            <td>{scaledText(valueOf(level, "quantity", "Quantity"), 100_000_000)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function TradesTable({ trades }) {
  const rows = trades.slice(-10).reverse();
  return (
    <table className="data-table">
      <thead>
        <tr>
          <th>Seq</th>
          <th>Price</th>
          <th>Qty</th>
        </tr>
      </thead>
      <tbody>
        {rows.length === 0 ? (
          <tr><td className="empty" colSpan="3">No trades</td></tr>
        ) : rows.map((trade, index) => (
          <tr key={`${valueOf(trade, "sequence", "Sequence")}-${index}`}>
            <td>{numberText(valueOf(trade, "sequence", "Sequence"))}</td>
            <td>{numberText(valueOf(trade, "price", "Price"))}</td>
            <td>{numberText(valueOf(trade, "quantity", "Quantity"))}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function EventLog({ events }) {
  if (events.length === 0) return <div className="empty-log">No events</div>;
  return (
    <div className="event-log">
      {events.map((event, index) => (
        <div className="event-row" key={`${valueOf(event, "sequence", "Sequence")}-${valueOf(event, "type", "Type")}-${index}`}>
          <span>{numberText(valueOf(event, "sequence", "Sequence"))}</span>
          <strong>{valueOf(event, "type", "Type")}</strong>
        </div>
      ))}
    </div>
  );
}

createRoot(document.getElementById("root")).render(<App />);
