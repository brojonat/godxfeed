// A queue with a maximum length. When the queue is full and a new item is added,
// the oldest item is removed.
export class MaxLengthQueue {
  constructor(maxLength) {
    this.maxLength = maxLength;
    this.queue = [];
  }

  enqueue(item) {
    // Add new item to the end of the queue
    this.queue.push(item);

    // If we exceed maxLength, remove from the front (oldest data)
    if (this.queue.length > this.maxLength) {
      this.queue.shift(); // Remove the first/oldest element
    }
  }

  dequeue() {
    if (this.queue.length > 0) {
      return this.queue.shift();
    }
    return null; // Or throw an error depending on how you want to handle empty queue cases
  }

  front() {
    return this.queue.length > 0 ? this.queue[0] : null;
  }

  back() {
    return this.queue.length > 0 ? this.queue[this.queue.length - 1] : null;
  }

  isEmpty() {
    return this.queue.length === 0;
  }

  length() {
    return this.queue.length;
  }
}
