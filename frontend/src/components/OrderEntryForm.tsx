function OrderEntryForm() {
return (
    <div>
        <h1>Order Entry Form</h1>
        <label>Symbol</label>
        <input type="text" />

        <label>Buy/Sell</label>

        <select>
            <option>Buy</option>
            <option>Sell</option>
        </select>
        <label>Quantity</label>
        <input type="number" />

        <label>Limit Price</label>
        <input type="number" />

        <button>Submit</button>
        
    </div>
    );
}

export default OrderEntryForm;